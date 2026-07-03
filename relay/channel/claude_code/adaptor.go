package claude_code

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/claude"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// mergeBetaHeaders merges required beta tokens with any client-supplied betas,
// preserving order and deduplicating.
func mergeBetaHeaders(required []string, incoming string) string {
	seen := make(map[string]struct{}, len(required)+8)
	out := make([]string, 0, len(required)+8)
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		if _, ok := seen[v]; ok {
			return
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	for _, r := range required {
		add(r)
	}
	for _, p := range strings.Split(incoming, ",") {
		add(p)
	}
	return strings.Join(out, ",")
}

// Adaptor handles Claude Code channels which use OAuth Bearer token authentication
// against the standard Anthropic API endpoint.
type Adaptor struct {
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {
}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	requestURL := fmt.Sprintf("%s/v1/messages", info.ChannelBaseUrl)
	if !shouldAppendClaudeBetaQuery(info) {
		return requestURL, nil
	}
	parsedURL, err := url.Parse(requestURL)
	if err != nil {
		return "", err
	}
	query := parsedURL.Query()
	query.Set("beta", "true")
	parsedURL.RawQuery = query.Encode()
	return parsedURL.String(), nil
}

func shouldAppendClaudeBetaQuery(info *relaycommon.RelayInfo) bool {
	if info == nil {
		return false
	}
	if info.IsClaudeBetaQuery {
		return true
	}
	if info.ChannelOtherSettings.ClaudeBetaQuery {
		return true
	}
	return false
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, req)

	accessToken, _, err := service.ParseClaudeCodeOAuthKey(info.ApiKey)
	if err != nil {
		accessToken = info.ApiKey
	}
	req.Set("Authorization", "Bearer "+accessToken)
	req.Del("x-api-key")

	req.Set("anthropic-version", "2023-06-01")

	// Merge required betas with any client-supplied betas.
	// claude-code-20250219 MUST be present: without it Anthropic treats the request
	// as a third-party API call and applies different (stricter) limits, causing 429s
	// on Max-plan tokens.
	clientBeta := c.Request.Header.Get("anthropic-beta")
	req.Set("anthropic-beta", mergeBetaHeaders(defaultInferenceBetas, clientBeta))

	// Impersonate the Claude Code CLI so Anthropic applies plan-level rate limits.
	for k, v := range cliHeaders {
		req.Set(k, v)
	}
	return nil
}

func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}
	claudeReq, err := claude.RequestOpenAI2ClaudeMessage(c, *request)
	if err != nil {
		return nil, err
	}
	injectClaudeCodeMimicry(claudeReq, info.ApiKey)
	return claudeReq, nil
}

// injectClaudeCodeMimicry injects the billing attribution system block and metadata.user_id
// that Anthropic requires to recognise a request as originating from the Claude Code CLI.
// Without these fields Anthropic applies third-party rate limits, causing 429 on Max-plan tokens.
func injectClaudeCodeMimicry(req *dto.ClaudeRequest, apiKey string) {
	// --- metadata.user_id ---
	// CLI >= 2.1.78 uses JSON format: {"device_id":"<64hex>","account_uuid":"<uuid>","session_id":"<uuid>"}
	// We impersonate cliVersion (2.1.161 >= 2.1.78), so we must use JSON format.
	// Using legacy format (user_xxx_account_xxx_session_xxx) with a 2.1.x UA causes
	// format inconsistency that Anthropic detects as a non-CC client.
	accountUUID := "00000000-0000-0000-0000-000000000000"
	if meta, err := service.ParseClaudeCodeOAuthKeyMeta(apiKey); err == nil && meta.AccountUUID != "" {
		accountUUID = meta.AccountUUID
	}
	deviceID := newHex32()   // 64 hex chars
	sessionUUID := newUUID() // 36 char UUID
	// user_id must be a JSON string (Anthropic validates "Input should be a valid string").
	// For CLI >= 2.1.78 the string value itself is a JSON object; for older versions it's the
	// legacy "user_xxx_account_xxx_session_xxx" form. We impersonate 2.1.161 → JSON-string form.
	userIDObjBytes, _ := common.Marshal(map[string]string{
		"device_id":    deviceID,
		"account_uuid": accountUUID,
		"session_id":   sessionUUID,
	})
	userIDStr := string(userIDObjBytes) // string whose content is a JSON object
	metaRaw, err := common.Marshal(map[string]string{"user_id": userIDStr})
	if err == nil {
		// Merge with existing metadata to avoid dropping client-supplied fields.
		if len(req.Metadata) > 0 {
			var existing map[string]json.RawMessage
			if common.Unmarshal(req.Metadata, &existing) == nil {
				if _, has := existing["user_id"]; !has {
					// Encode the string value properly so it remains a JSON string.
					encodedStr, err2 := common.Marshal(userIDStr)
					if err2 == nil {
						existing["user_id"] = encodedStr
						if merged, err3 := common.Marshal(existing); err3 == nil {
							metaRaw = merged
						}
					}
				} else {
					metaRaw = req.Metadata // keep client-supplied user_id
				}
			}
		}
		req.Metadata = metaRaw
	}

	// --- system blocks ---
	// Prepend the 3-block system structure that matches real Claude Code CLI:
	//   [0] billing attribution block (x-anthropic-billing-header: cc_version={ver}.{fp}; ...)
	//   [1] identity block ("You are Claude Code...")
	//   [2] expansion block (generic CLI prompt excerpt, makes block count match real CLI)
	// The fingerprint in the billing block is computed from the first user message text.
	fp := computeFingerprint(req.Messages, cliVersion)
	billingText := fmt.Sprintf(claudeCodeBillingBlockFmt, cliVersion, fp)
	billingBlock := map[string]any{"type": "text", "text": billingText}
	identityBlock := map[string]any{"type": "text", "text": claudeCodeIdentityPrompt}
	expansionBlock := map[string]any{"type": "text", "text": claudeCodeExpansionPrompt}

	prefix := []any{billingBlock, identityBlock, expansionBlock}

	switch {
	case req.System == nil:
		req.System = prefix
	case req.IsStringSystem():
		orig := req.GetStringSystem()
		if orig == "" {
			req.System = prefix
		} else {
			req.System = append(prefix, map[string]any{"type": "text", "text": orig})
		}
	default:
		existing := req.ParseSystem()
		blocks := make([]any, 0, len(prefix)+len(existing))
		blocks = append(blocks, prefix...)
		for _, b := range existing {
			blocks = append(blocks, b)
		}
		req.System = blocks
	}
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// newHex32 returns 32 cryptographically-random bytes encoded as 64 lowercase hex chars.
// Used as the device_id component of the JSON-format metadata.user_id.
func newHex32() string {
	var b [32]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%x", b)
}

// computeFingerprint replicates the real Claude Code CLI's cc_version fingerprint algorithm:
// SHA256(salt + chars_at_4_7_20_of_first_user_text + version), take first 3 hex chars.
func computeFingerprint(messages []dto.ClaudeMessage, version string) string {
	var firstText string
	for _, msg := range messages {
		if msg.Role == "user" {
			firstText = msg.GetStringContent()
			break
		}
	}
	indices := []int{4, 7, 20}
	chars := make([]byte, 0, 3)
	for _, i := range indices {
		if i < len(firstText) {
			chars = append(chars, firstText[i])
		} else {
			chars = append(chars, '0')
		}
	}
	sum := sha256.Sum256([]byte(fingerprintSalt + string(chars) + version))
	return hex.EncodeToString(sum[:])[:3]
}

func (a *Adaptor) ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) (any, error) {
	return request, nil
}

func (a *Adaptor) ConvertGeminiRequest(*gin.Context, *relaycommon.RelayInfo, *dto.GeminiChatRequest) (any, error) {
	return nil, errors.New("claude code channel: gemini endpoint not supported")
}

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	return nil, errors.New("claude code channel: audio endpoint not supported")
}

func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	return nil, errors.New("claude code channel: image endpoint not supported")
}

func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return nil, errors.New("claude code channel: rerank endpoint not supported")
}

func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	return nil, errors.New("claude code channel: embedding endpoint not supported")
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	return nil, errors.New("claude code channel: responses endpoint not supported")
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	return channel.DoApiRequest(a, c, info, requestBody)
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	info.FinalRequestRelayFormat = types.RelayFormatClaude
	if info.IsStream {
		return claude.ClaudeStreamHandler(c, resp, info)
	}
	return claude.ClaudeHandler(c, resp, info)
}

func (a *Adaptor) GetModelList() []string {
	return claude.ModelList
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}
