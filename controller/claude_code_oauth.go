package controller

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

func claudeCodeOAuthSessionKey(channelID int, field string) string {
	return fmt.Sprintf("claude_code_oauth_%s_%d", field, channelID)
}

func StartClaudeCodeOAuth(c *gin.Context) {
	startClaudeCodeOAuthWithChannelID(c, 0)
}

func StartClaudeCodeOAuthForChannel(c *gin.Context) {
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, fmt.Errorf("invalid channel id: %w", err))
		return
	}
	startClaudeCodeOAuthWithChannelID(c, channelID)
}

func startClaudeCodeOAuthWithChannelID(c *gin.Context, channelID int) {
	if channelID > 0 {
		ch, err := model.GetChannelById(channelID, false)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if ch == nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "channel not found"})
			return
		}
		if ch.Type != constant.ChannelTypeClaudeCode {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "channel type is not ClaudeCode"})
			return
		}
	}

	flow, err := service.CreateClaudeCodeOAuthAuthorizationFlow()
	if err != nil {
		common.ApiError(c, err)
		return
	}

	session := sessions.Default(c)
	session.Set(claudeCodeOAuthSessionKey(channelID, "state"), flow.State)
	session.Set(claudeCodeOAuthSessionKey(channelID, "verifier"), flow.Verifier)
	session.Set(claudeCodeOAuthSessionKey(channelID, "created_at"), time.Now().Unix())
	_ = session.Save()

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"authorize_url": flow.AuthorizeURL,
		},
	})
}

func CompleteClaudeCodeOAuth(c *gin.Context) {
	completeClaudeCodeOAuthWithChannelID(c, 0)
}

func CompleteClaudeCodeOAuthForChannel(c *gin.Context) {
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, fmt.Errorf("invalid channel id: %w", err))
		return
	}
	completeClaudeCodeOAuthWithChannelID(c, channelID)
}

type claudeCodeOAuthCompleteRequest struct {
	Input string `json:"input"`
}

func completeClaudeCodeOAuthWithChannelID(c *gin.Context, channelID int) {
	req := claudeCodeOAuthCompleteRequest{}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}

	code, state, err := service.ParseClaudeCodeAuthorizationInput(req.Input)
	if err != nil {
		common.SysError("failed to parse claude code authorization input: " + err.Error())
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "解析授权信息失败，请检查输入格式"})
		return
	}
	if strings.TrimSpace(code) == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "missing authorization code"})
		return
	}
	if strings.TrimSpace(state) == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "missing state in callback URL"})
		return
	}

	channelProxy := ""
	if channelID > 0 {
		ch, err := model.GetChannelById(channelID, false)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if ch == nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "channel not found"})
			return
		}
		if ch.Type != constant.ChannelTypeClaudeCode {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "channel type is not ClaudeCode"})
			return
		}
		channelProxy = ch.GetSetting().Proxy
	}

	session := sessions.Default(c)
	expectedState, _ := session.Get(claudeCodeOAuthSessionKey(channelID, "state")).(string)
	verifier, _ := session.Get(claudeCodeOAuthSessionKey(channelID, "verifier")).(string)
	if strings.TrimSpace(expectedState) == "" || strings.TrimSpace(verifier) == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "oauth flow not started or session expired"})
		return
	}
	if state != expectedState {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "state mismatch"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()

	tokenRes, err := service.ExchangeClaudeCodeAuthorizationCodeWithProxy(ctx, code, verifier, state, channelProxy)
	if err != nil {
		common.SysError("failed to exchange claude code authorization code: " + err.Error())
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "授权码交换失败：" + err.Error()})
		return
	}

	keyJSON, err := service.BuildClaudeCodeOAuthKey(tokenRes)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	session.Delete(claudeCodeOAuthSessionKey(channelID, "state"))
	session.Delete(claudeCodeOAuthSessionKey(channelID, "verifier"))
	session.Delete(claudeCodeOAuthSessionKey(channelID, "created_at"))
	_ = session.Save()

	if channelID > 0 {
		if err := model.DB.Model(&model.Channel{}).Where("id = ?", channelID).Update("key", keyJSON).Error; err != nil {
			common.ApiError(c, err)
			return
		}
		model.InitChannelCache()
		service.ResetProxyClientCache()
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "saved",
			"data": gin.H{
				"channel_id":   channelID,
				"account_uuid": tokenRes.AccountUUID,
				"email":        tokenRes.Email,
				"expires_at":   tokenRes.ExpiresAt.Format(time.RFC3339),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "generated",
		"data": gin.H{
			"key":          keyJSON,
			"account_uuid": tokenRes.AccountUUID,
			"email":        tokenRes.Email,
			"expires_at":   tokenRes.ExpiresAt.Format(time.RFC3339),
		},
	})
}

func RefreshClaudeCodeChannelCredential(c *gin.Context) {
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, fmt.Errorf("invalid channel id: %w", err))
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 25*time.Second)
	defer cancel()

	tokenRes, ch, err := service.RefreshClaudeCodeChannelCredential(ctx, channelID, service.ClaudeCodeCredentialRefreshOptions{ResetCaches: true})
	if err != nil {
		common.SysError("failed to refresh claude code oauth token: " + err.Error())
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "token refresh failed: " + err.Error()})
		return
	}

	expiresAt := ""
	if !tokenRes.ExpiresAt.IsZero() {
		expiresAt = tokenRes.ExpiresAt.Format(time.RFC3339)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "refreshed",
		"data": gin.H{
			"channel_id":   ch.Id,
			"account_uuid": tokenRes.AccountUUID,
			"email":        tokenRes.Email,
			"expires_at":   expiresAt,
			"last_refresh": time.Now().Format(time.RFC3339),
		},
	})
}
