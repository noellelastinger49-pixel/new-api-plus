package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const (
	claudeCodeOAuthClientID     = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	claudeCodeOAuthAuthorizeURL = "https://claude.ai/oauth/authorize"
	claudeCodeOAuthTokenURL     = "https://platform.claude.com/v1/oauth/token"
	claudeCodeOAuthRedirectURI  = "https://platform.claude.com/oauth/code/callback"
	claudeCodeOAuthScope        = "org:create_api_key user:profile user:inference user:sessions:claude_code user:mcp_servers user:file_upload"
)

type ClaudeCodeOAuthTokenResult struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	AccountUUID  string
	OrgUUID      string
	Email        string
}

type ClaudeCodeOAuthAuthorizationFlow struct {
	State        string
	Verifier     string
	Challenge    string
	AuthorizeURL string
}

func CreateClaudeCodeOAuthAuthorizationFlow() (*ClaudeCodeOAuthAuthorizationFlow, error) {
	state, err := createStateHex(16)
	if err != nil {
		return nil, err
	}
	verifier, challenge, err := generatePKCEPair()
	if err != nil {
		return nil, err
	}
	u, err := buildClaudeCodeAuthorizeURL(state, challenge)
	if err != nil {
		return nil, err
	}
	return &ClaudeCodeOAuthAuthorizationFlow{
		State:        state,
		Verifier:     verifier,
		Challenge:    challenge,
		AuthorizeURL: u,
	}, nil
}

func ExchangeClaudeCodeAuthorizationCode(ctx context.Context, code string, verifier string, state string) (*ClaudeCodeOAuthTokenResult, error) {
	return ExchangeClaudeCodeAuthorizationCodeWithProxy(ctx, code, verifier, state, "")
}

func ExchangeClaudeCodeAuthorizationCodeWithProxy(ctx context.Context, code string, verifier string, state string, proxyURL string) (*ClaudeCodeOAuthTokenResult, error) {
	client, err := getClaudeCodeOAuthHTTPClient(proxyURL)
	if err != nil {
		return nil, err
	}
	return exchangeClaudeCodeAuthorizationCode(ctx, client, code, verifier, state)
}

func RefreshClaudeCodeOAuthToken(ctx context.Context, refreshToken string) (*ClaudeCodeOAuthTokenResult, error) {
	return RefreshClaudeCodeOAuthTokenWithProxy(ctx, refreshToken, "")
}

func RefreshClaudeCodeOAuthTokenWithProxy(ctx context.Context, refreshToken string, proxyURL string) (*ClaudeCodeOAuthTokenResult, error) {
	client, err := getClaudeCodeOAuthHTTPClient(proxyURL)
	if err != nil {
		return nil, err
	}
	return refreshClaudeCodeOAuthToken(ctx, client, refreshToken)
}

func buildClaudeCodeAuthorizeURL(state string, challenge string) (string, error) {
	encodedRedirectURI := url.QueryEscape(claudeCodeOAuthRedirectURI)
	encodedScope := strings.ReplaceAll(url.QueryEscape(claudeCodeOAuthScope), "%20", "+")

	rawURL := fmt.Sprintf("%s?code=true&client_id=%s&response_type=code&redirect_uri=%s&scope=%s&code_challenge=%s&code_challenge_method=S256&state=%s",
		claudeCodeOAuthAuthorizeURL,
		claudeCodeOAuthClientID,
		encodedRedirectURI,
		encodedScope,
		challenge,
		state,
	)
	return rawURL, nil
}

func exchangeClaudeCodeAuthorizationCode(ctx context.Context, client *http.Client, code string, verifier string, state string) (*ClaudeCodeOAuthTokenResult, error) {
	authCode := strings.TrimSpace(code)
	if authCode == "" {
		return nil, errors.New("empty authorization code")
	}
	v := strings.TrimSpace(verifier)
	if v == "" {
		return nil, errors.New("empty code_verifier")
	}

	reqBody := map[string]any{
		"code":          authCode,
		"grant_type":    "authorization_code",
		"client_id":     claudeCodeOAuthClientID,
		"redirect_uri":  claudeCodeOAuthRedirectURI,
		"code_verifier": v,
	}
	if s := strings.TrimSpace(state); s != "" {
		reqBody["state"] = s
	}

	bodyBytes, err := common.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, claudeCodeOAuthTokenURL, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("User-Agent", "axios/1.13.6")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("claude code oauth token exchange failed: status=%d body=%s", resp.StatusCode, string(respBytes))
	}

	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		Organization *struct {
			UUID string `json:"uuid"`
		} `json:"organization"`
		Account *struct {
			UUID         string `json:"uuid"`
			EmailAddress string `json:"email_address"`
		} `json:"account"`
	}

	if err := common.Unmarshal(respBytes, &payload); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	if strings.TrimSpace(payload.AccessToken) == "" {
		return nil, errors.New("claude code oauth token response missing access_token")
	}

	result := &ClaudeCodeOAuthTokenResult{
		AccessToken:  strings.TrimSpace(payload.AccessToken),
		RefreshToken: strings.TrimSpace(payload.RefreshToken),
	}
	if payload.ExpiresIn > 0 {
		result.ExpiresAt = time.Now().Add(time.Duration(payload.ExpiresIn) * time.Second)
	}
	if payload.Organization != nil {
		result.OrgUUID = payload.Organization.UUID
	}
	if payload.Account != nil {
		result.AccountUUID = payload.Account.UUID
		result.Email = payload.Account.EmailAddress
	}

	return result, nil
}

func refreshClaudeCodeOAuthToken(ctx context.Context, client *http.Client, refreshToken string) (*ClaudeCodeOAuthTokenResult, error) {
	rt := strings.TrimSpace(refreshToken)
	if rt == "" {
		return nil, errors.New("empty refresh_token")
	}

	reqBody := map[string]any{
		"grant_type":    "refresh_token",
		"refresh_token": rt,
		"client_id":     claudeCodeOAuthClientID,
	}

	bodyBytes, err := common.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, claudeCodeOAuthTokenURL, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("User-Agent", "axios/1.13.6")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}

	if err := common.DecodeJson(resp.Body, &payload); err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("claude code oauth refresh failed: status=%d", resp.StatusCode)
	}
	if strings.TrimSpace(payload.AccessToken) == "" {
		return nil, errors.New("claude code oauth refresh response missing access_token")
	}

	result := &ClaudeCodeOAuthTokenResult{
		AccessToken:  strings.TrimSpace(payload.AccessToken),
		RefreshToken: strings.TrimSpace(payload.RefreshToken),
	}
	if payload.ExpiresIn > 0 {
		result.ExpiresAt = time.Now().Add(time.Duration(payload.ExpiresIn) * time.Second)
	}
	return result, nil
}

func getClaudeCodeOAuthHTTPClient(proxyURL string) (*http.Client, error) {
	baseClient, err := GetHttpClientWithProxy(strings.TrimSpace(proxyURL))
	if err != nil {
		return nil, err
	}
	if baseClient == nil {
		return &http.Client{Timeout: defaultHTTPTimeout}, nil
	}
	clientCopy := *baseClient
	clientCopy.Timeout = defaultHTTPTimeout
	return &clientCopy, nil
}

func ParseClaudeCodeAuthorizationInput(input string) (code string, state string, err error) {
	v := strings.TrimSpace(input)
	if v == "" {
		return "", "", errors.New("empty input")
	}
	// Handle full callback URL
	if strings.Contains(v, "code=") {
		u, parseErr := url.Parse(v)
		if parseErr == nil {
			q := u.Query()
			code = strings.TrimSpace(q.Get("code"))
			state = strings.TrimSpace(q.Get("state"))
			if code != "" {
				return code, state, nil
			}
		}
	}
	// Handle "code#state" format
	if strings.Contains(v, "#") {
		parts := strings.SplitN(v, "#", 2)
		code = strings.TrimSpace(parts[0])
		state = strings.TrimSpace(parts[1])
		return code, state, nil
	}
	// Plain code
	code = v
	return code, "", nil
}

// ExtractEmailFromClaudeCodeJWT extracts email from JWT access token (same JWT structure as Codex)
func ExtractEmailFromClaudeCodeJWT(token string) (string, bool) {
	return ExtractEmailFromJWT(token)
}

// ClaudeCodeOAuthKeyJSON is the JSON structure for Claude Code channel key.
// The access_token is used as the Bearer token for Anthropic API calls.
type ClaudeCodeOAuthKeyJSON struct {
	raw map[string]json.RawMessage
}

// BuildClaudeCodeOAuthKey serializes the OAuth credentials as JSON for storage as the channel key.
func BuildClaudeCodeOAuthKey(result *ClaudeCodeOAuthTokenResult) (string, error) {
	key := map[string]any{
		"access_token":  result.AccessToken,
		"refresh_token": result.RefreshToken,
		"account_uuid":  result.AccountUUID,
		"org_uuid":      result.OrgUUID,
		"email":         result.Email,
		"last_refresh":  time.Now().Format(time.RFC3339),
		"type":          "claude_code",
	}
	if !result.ExpiresAt.IsZero() {
		key["expires_at"] = result.ExpiresAt.Format(time.RFC3339)
	}
	b, err := common.Marshal(key)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ClaudeCodeOAuthKeyMeta holds parsed fields from a stored Claude Code OAuth key.
type ClaudeCodeOAuthKeyMeta struct {
	AccessToken  string
	RefreshToken string
	AccountUUID  string
	OrgUUID      string
	Email        string
}

// claudeAiOauthKeyEnvelope models the Claude Code CLI credential format:
// {"claudeAiOauth":{"accessToken":...,"refreshToken":...,"expiresAt":<unix_ms>,...}}
type claudeAiOauthKeyEnvelope struct {
	ClaudeAiOauth *claudeAiOauthCredentials `json:"claudeAiOauth"`
}

type claudeAiOauthCredentials struct {
	AccessToken      string   `json:"accessToken"`
	RefreshToken     string   `json:"refreshToken"`
	ExpiresAt        int64    `json:"expiresAt"` // unix epoch milliseconds
	Scopes           []string `json:"scopes,omitempty"`
	SubscriptionType string   `json:"subscriptionType,omitempty"`
	RateLimitTier    string   `json:"rateLimitTier,omitempty"`
}

// claudeCodeParsedKey is the normalized view of a stored Claude Code channel key,
// regardless of which on-disk format it was written in.
type claudeCodeParsedKey struct {
	AccessToken      string
	RefreshToken     string
	AccountUUID      string
	OrgUUID          string
	Email            string
	ExpiresAt        time.Time
	IsOAuthWrapper   bool
	Scopes           []string
	SubscriptionType string
	RateLimitTier    string
}

// parseClaudeCodeKey parses a stored Claude Code channel key, accepting both the
// Claude Code CLI's claudeAiOauth wrapper format and the internal snake_case format.
func parseClaudeCodeKey(raw string) (*claudeCodeParsedKey, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, errors.New("empty claude code oauth key")
	}

	// Claude Code CLI credential format: {"claudeAiOauth":{...}}
	var envelope claudeAiOauthKeyEnvelope
	if err := common.Unmarshal([]byte(trimmed), &envelope); err == nil && envelope.ClaudeAiOauth != nil {
		creds := envelope.ClaudeAiOauth
		accessToken := strings.TrimSpace(creds.AccessToken)
		if accessToken == "" {
			return nil, errors.New("claude code oauth: missing accessToken in claudeAiOauth key")
		}
		parsed := &claudeCodeParsedKey{
			AccessToken:      accessToken,
			RefreshToken:     strings.TrimSpace(creds.RefreshToken),
			IsOAuthWrapper:   true,
			Scopes:           creds.Scopes,
			SubscriptionType: strings.TrimSpace(creds.SubscriptionType),
			RateLimitTier:    strings.TrimSpace(creds.RateLimitTier),
		}
		if creds.ExpiresAt > 0 {
			parsed.ExpiresAt = time.UnixMilli(creds.ExpiresAt)
		}
		return parsed, nil
	}

	// Internal format: {"access_token":...,"expires_at":"<RFC3339>",...}
	var key struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		AccountUUID  string `json:"account_uuid"`
		OrgUUID      string `json:"org_uuid"`
		Email        string `json:"email"`
		ExpiresAt    string `json:"expires_at"`
	}
	if err := common.Unmarshal([]byte(trimmed), &key); err != nil {
		return nil, fmt.Errorf("claude code oauth: invalid key json: %w", err)
	}
	if strings.TrimSpace(key.AccessToken) == "" {
		return nil, errors.New("claude code oauth: missing access_token in key")
	}
	parsed := &claudeCodeParsedKey{
		AccessToken:  strings.TrimSpace(key.AccessToken),
		RefreshToken: strings.TrimSpace(key.RefreshToken),
		AccountUUID:  strings.TrimSpace(key.AccountUUID),
		OrgUUID:      strings.TrimSpace(key.OrgUUID),
		Email:        strings.TrimSpace(key.Email),
	}
	if exp := strings.TrimSpace(key.ExpiresAt); exp != "" {
		if t, err := time.Parse(time.RFC3339, exp); err == nil {
			parsed.ExpiresAt = t
		}
	}
	return parsed, nil
}

// ParseClaudeCodeOAuthKeyMeta parses the stored JSON key and returns all metadata fields.
func ParseClaudeCodeOAuthKeyMeta(raw string) (*ClaudeCodeOAuthKeyMeta, error) {
	parsed, err := parseClaudeCodeKey(raw)
	if err != nil {
		return nil, err
	}
	return &ClaudeCodeOAuthKeyMeta{
		AccessToken:  parsed.AccessToken,
		RefreshToken: parsed.RefreshToken,
		AccountUUID:  parsed.AccountUUID,
		OrgUUID:      parsed.OrgUUID,
		Email:        parsed.Email,
	}, nil
}

// ParseClaudeCodeOAuthKey parses the stored JSON key and returns the access_token.
func ParseClaudeCodeOAuthKey(raw string) (accessToken string, refreshToken string, err error) {
	parsed, err := parseClaudeCodeKey(raw)
	if err != nil {
		return "", "", err
	}
	return parsed.AccessToken, parsed.RefreshToken, nil
}

// GetClaudeCodeKeyExpiry returns the credential expiry parsed from a stored Claude Code
// channel key (either the claudeAiOauth CLI format or the internal format). The boolean
// is false when the key is unparsable or carries no expiry information.
func GetClaudeCodeKeyExpiry(raw string) (time.Time, bool) {
	parsed, err := parseClaudeCodeKey(raw)
	if err != nil || parsed.ExpiresAt.IsZero() {
		return time.Time{}, false
	}
	return parsed.ExpiresAt, true
}

// BuildClaudeCodeOAuthKeyPreservingFormat re-serializes refreshed OAuth credentials using
// the same on-disk format as the previous key. A key originally supplied in the Claude Code
// CLI's claudeAiOauth wrapper stays in that wrapper (updating accessToken, refreshToken and
// the millisecond expiresAt while preserving scopes/subscriptionType/rateLimitTier); any
// other key is written in the internal format via BuildClaudeCodeOAuthKey. When the refresh
// response omits a refresh token, the previous one is retained.
func BuildClaudeCodeOAuthKeyPreservingFormat(oldRaw string, result *ClaudeCodeOAuthTokenResult) (string, error) {
	if result == nil {
		return "", errors.New("nil claude code oauth token result")
	}
	prev, prevErr := parseClaudeCodeKey(oldRaw)

	refreshToken := strings.TrimSpace(result.RefreshToken)
	if refreshToken == "" && prevErr == nil {
		refreshToken = prev.RefreshToken
	}

	if prevErr == nil && prev.IsOAuthWrapper {
		creds := map[string]any{
			"accessToken":  result.AccessToken,
			"refreshToken": refreshToken,
		}
		if !result.ExpiresAt.IsZero() {
			creds["expiresAt"] = result.ExpiresAt.UnixMilli()
		}
		if len(prev.Scopes) > 0 {
			creds["scopes"] = prev.Scopes
		}
		if prev.SubscriptionType != "" {
			creds["subscriptionType"] = prev.SubscriptionType
		}
		if prev.RateLimitTier != "" {
			creds["rateLimitTier"] = prev.RateLimitTier
		}
		b, err := common.Marshal(map[string]any{"claudeAiOauth": creds})
		if err != nil {
			return "", err
		}
		return string(b), nil
	}

	// Internal format: preserve metadata carried by the previous key.
	merged := *result
	merged.RefreshToken = refreshToken
	if prevErr == nil {
		if merged.AccountUUID == "" {
			merged.AccountUUID = prev.AccountUUID
		}
		if merged.OrgUUID == "" {
			merged.OrgUUID = prev.OrgUUID
		}
		if merged.Email == "" {
			merged.Email = prev.Email
		}
	}
	return BuildClaudeCodeOAuthKey(&merged)
}

func createStateHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func generatePKCEPair() (verifier string, challenge string, err error) {
	verifier, err = createStateHex(32)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge, nil
}
