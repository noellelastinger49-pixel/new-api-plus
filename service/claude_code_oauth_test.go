package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseClaudeCodeKey_OAuthWrapperFormat(t *testing.T) {
	// Claude Code CLI credential format with millisecond expiresAt.
	raw := `{"claudeAiOauth":{"accessToken":"sk-ant-oat01-abc","refreshToken":"sk-ant-ort01-xyz","expiresAt":1782662007674,"scopes":["user:inference","user:profile"],"subscriptionType":"pro","rateLimitTier":"default_claude_ai"}}`

	parsed, err := parseClaudeCodeKey(raw)
	require.NoError(t, err)
	assert.True(t, parsed.IsOAuthWrapper)
	assert.Equal(t, "sk-ant-oat01-abc", parsed.AccessToken)
	assert.Equal(t, "sk-ant-ort01-xyz", parsed.RefreshToken)
	assert.Equal(t, "pro", parsed.SubscriptionType)
	assert.Equal(t, "default_claude_ai", parsed.RateLimitTier)
	assert.Equal(t, []string{"user:inference", "user:profile"}, parsed.Scopes)
	assert.Equal(t, time.UnixMilli(1782662007674), parsed.ExpiresAt)

	accessToken, refreshToken, err := ParseClaudeCodeOAuthKey(raw)
	require.NoError(t, err)
	assert.Equal(t, "sk-ant-oat01-abc", accessToken)
	assert.Equal(t, "sk-ant-ort01-xyz", refreshToken)

	expiry, ok := GetClaudeCodeKeyExpiry(raw)
	require.True(t, ok)
	assert.Equal(t, time.UnixMilli(1782662007674), expiry)
}

func TestParseClaudeCodeKey_InternalFormat(t *testing.T) {
	exp := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	raw := `{"access_token":"sk-ant-oat01-internal","refresh_token":"sk-ant-ort01-internal","account_uuid":"acc-123","org_uuid":"org-456","email":"user@example.com","expires_at":"` + exp.Format(time.RFC3339) + `","type":"claude_code"}`

	parsed, err := parseClaudeCodeKey(raw)
	require.NoError(t, err)
	assert.False(t, parsed.IsOAuthWrapper)
	assert.Equal(t, "sk-ant-oat01-internal", parsed.AccessToken)
	assert.Equal(t, "acc-123", parsed.AccountUUID)
	assert.Equal(t, "org-456", parsed.OrgUUID)
	assert.Equal(t, "user@example.com", parsed.Email)
	assert.True(t, parsed.ExpiresAt.Equal(exp))

	meta, err := ParseClaudeCodeOAuthKeyMeta(raw)
	require.NoError(t, err)
	assert.Equal(t, "acc-123", meta.AccountUUID)
	assert.Equal(t, "user@example.com", meta.Email)
}

func TestParseClaudeCodeKey_Invalid(t *testing.T) {
	_, err := parseClaudeCodeKey("")
	require.Error(t, err)

	_, err = parseClaudeCodeKey(`{"claudeAiOauth":{"refreshToken":"r"}}`)
	require.Error(t, err) // missing accessToken

	_, err = parseClaudeCodeKey(`{"refresh_token":"r"}`)
	require.Error(t, err) // missing access_token
}

func TestBuildClaudeCodeOAuthKeyPreservingFormat_OAuthWrapper(t *testing.T) {
	oldRaw := `{"claudeAiOauth":{"accessToken":"old-access","refreshToken":"old-refresh","expiresAt":1782662007674,"scopes":["user:inference"],"subscriptionType":"pro","rateLimitTier":"default_claude_ai"}}`

	newExpiry := time.UnixMilli(1799999999000)
	result := &ClaudeCodeOAuthTokenResult{
		AccessToken:  "new-access",
		RefreshToken: "new-refresh",
		ExpiresAt:    newExpiry,
	}

	out, err := BuildClaudeCodeOAuthKeyPreservingFormat(oldRaw, result)
	require.NoError(t, err)

	parsed, err := parseClaudeCodeKey(out)
	require.NoError(t, err)
	assert.True(t, parsed.IsOAuthWrapper, "refreshed key must stay in claudeAiOauth format")
	assert.Equal(t, "new-access", parsed.AccessToken)
	assert.Equal(t, "new-refresh", parsed.RefreshToken)
	assert.Equal(t, newExpiry, parsed.ExpiresAt)
	assert.Equal(t, "pro", parsed.SubscriptionType, "subscriptionType must be preserved")
	assert.Equal(t, "default_claude_ai", parsed.RateLimitTier, "rateLimitTier must be preserved")
	assert.Equal(t, []string{"user:inference"}, parsed.Scopes, "scopes must be preserved")
}

func TestBuildClaudeCodeOAuthKeyPreservingFormat_KeepsOldRefreshToken(t *testing.T) {
	oldRaw := `{"claudeAiOauth":{"accessToken":"old-access","refreshToken":"old-refresh","expiresAt":1782662007674}}`

	// Refresh response omitted the refresh token; the old one must be retained.
	result := &ClaudeCodeOAuthTokenResult{
		AccessToken: "new-access",
		ExpiresAt:   time.UnixMilli(1799999999000),
	}

	out, err := BuildClaudeCodeOAuthKeyPreservingFormat(oldRaw, result)
	require.NoError(t, err)

	parsed, err := parseClaudeCodeKey(out)
	require.NoError(t, err)
	assert.Equal(t, "new-access", parsed.AccessToken)
	assert.Equal(t, "old-refresh", parsed.RefreshToken)
}

func TestBuildClaudeCodeOAuthKeyPreservingFormat_InternalFormatPreservesMetadata(t *testing.T) {
	oldRaw := `{"access_token":"old-access","refresh_token":"old-refresh","account_uuid":"acc-123","org_uuid":"org-456","email":"user@example.com","type":"claude_code"}`

	result := &ClaudeCodeOAuthTokenResult{
		AccessToken:  "new-access",
		RefreshToken: "new-refresh",
		ExpiresAt:    time.Now().Add(time.Hour),
	}

	out, err := BuildClaudeCodeOAuthKeyPreservingFormat(oldRaw, result)
	require.NoError(t, err)

	parsed, err := parseClaudeCodeKey(out)
	require.NoError(t, err)
	assert.False(t, parsed.IsOAuthWrapper, "internal-format key must stay internal format")
	assert.Equal(t, "new-access", parsed.AccessToken)
	assert.Equal(t, "acc-123", parsed.AccountUUID, "account_uuid must be preserved")
	assert.Equal(t, "org-456", parsed.OrgUUID, "org_uuid must be preserved")
	assert.Equal(t, "user@example.com", parsed.Email, "email must be preserved")
}
