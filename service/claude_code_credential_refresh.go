package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
)

type ClaudeCodeCredentialRefreshOptions struct {
	ResetCaches bool
}

// RefreshClaudeCodeChannelCredential refreshes the OAuth access token for a Claude Code
// channel using its stored refresh token, persists the new key (preserving the original
// key format), and optionally resets the channel/proxy caches. It returns the refreshed
// token result and the channel.
func RefreshClaudeCodeChannelCredential(ctx context.Context, channelID int, opts ClaudeCodeCredentialRefreshOptions) (*ClaudeCodeOAuthTokenResult, *model.Channel, error) {
	ch, err := model.GetChannelById(channelID, true)
	if err != nil {
		return nil, nil, err
	}
	if ch == nil {
		return nil, nil, fmt.Errorf("channel not found")
	}
	if ch.Type != constant.ChannelTypeClaudeCode {
		return nil, nil, fmt.Errorf("channel type is not ClaudeCode")
	}

	currentKey := strings.TrimSpace(ch.Key)
	_, refreshToken, err := ParseClaudeCodeOAuthKey(currentKey)
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(refreshToken) == "" {
		return nil, nil, fmt.Errorf("claude code channel: refresh_token is required to refresh credential")
	}

	refreshCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	tokenRes, err := RefreshClaudeCodeOAuthTokenWithProxy(refreshCtx, refreshToken, ch.GetSetting().Proxy)
	if err != nil {
		return nil, nil, err
	}

	keyJSON, err := BuildClaudeCodeOAuthKeyPreservingFormat(currentKey, tokenRes)
	if err != nil {
		return nil, nil, err
	}

	if err := model.DB.Model(&model.Channel{}).Where("id = ?", ch.Id).Update("key", keyJSON).Error; err != nil {
		return nil, nil, err
	}

	if opts.ResetCaches {
		model.InitChannelCache()
		ResetProxyClientCache()
	}

	return tokenRes, ch, nil
}
