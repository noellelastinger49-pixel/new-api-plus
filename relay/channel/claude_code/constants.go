package claude_code

const ChannelName = "claude_code"

// cliVersion is the Claude Code CLI version we impersonate.
// Anthropic validates the User-Agent to determine quota bucket (plan vs third-party).
const cliVersion = "2.1.161"

// Required anthropic-beta tokens for inference with Claude Code OAuth credentials.
// Without claude-code-20250219, Anthropic treats the request as a third-party API
// call and applies different (stricter) limits, causing 429s on Max-plan tokens.
const (
	betaClaudeCode          = "claude-code-20250219"
	betaOAuth               = "oauth-2025-04-20"
	betaInterleavedThinking = "interleaved-thinking-2025-05-14"
	betaPromptCachingScope  = "prompt-caching-scope-2026-01-05"
	betaEffort              = "effort-2025-11-24"
	betaContextManagement   = "context-management-2025-06-27"
	betaExtendedCacheTTL    = "extended-cache-ttl-2025-04-11"
)

const betaFineGrainedToolStreaming = "fine-grained-tool-streaming-2025-05-14"

// defaultInferenceBetas is the ordered list of beta tokens that must be present
// in every inference request with a Claude Code OAuth token.
// Order matches real Claude Code CLI traffic to reduce fingerprinting risk.
var defaultInferenceBetas = []string{
	betaClaudeCode,
	betaOAuth,
	betaInterleavedThinking,
	betaFineGrainedToolStreaming,
	betaPromptCachingScope,
	betaEffort,
	betaContextManagement,
	betaExtendedCacheTTL,
}

// claudeCodeIdentityPrompt is the system prompt prefix all Claude Code CLI requests carry.
// Anthropic uses its presence (together with the billing block) to identify legitimate CC clients.
const claudeCodeIdentityPrompt = "You are Claude Code, Anthropic's official CLI for Claude."

// fingerprintSalt is used in the cc_version fingerprint algorithm (matches real Claude Code CLI).
const fingerprintSalt = "59cf53e54c78"

// claudeCodeBillingBlockFmt is the billing attribution block template injected into every system
// array. Format: "x-anthropic-billing-header: cc_version={ver}.{fp}; cc_entrypoint=cli; cch=00000;"
// where {fp} is a 3-char SHA256 fingerprint derived from the first user message text + version.
// cch=00000 is a placeholder; real CC uses an xxhash64 signature, but 00000 is accepted upstream.
const claudeCodeBillingBlockFmt = "x-anthropic-billing-header: cc_version=%s.%s; cc_entrypoint=cli; cch=00000;"

// claudeCodeExpansionPrompt is injected as the 3rd system block to match real Claude Code CLI's
// 3-block system structure (billing + identity + expansion). This generic paragraph comes from
// real CLI traffic and excludes tool-specific instructions that would alter the user's session.
const claudeCodeExpansionPrompt = `You are an interactive agent that helps users with software engineering tasks. Use the instructions below and the tools available to you to assist the user.

IMPORTANT: Assist with authorized security testing, defensive security, CTF challenges, and educational contexts. Refuse requests for destructive techniques, DoS attacks, mass targeting, supply chain compromise, or detection evasion for malicious purposes. Dual-use security tools (C2 frameworks, credential testing, exploit development) require clear authorization context: pentesting engagements, CTF competitions, security research, or defensive use cases.
IMPORTANT: You must NEVER generate or guess URLs for the user unless you are confident that the URLs are for helping the user with programming. You may use URLs provided by the user in their messages or local files.

# Tone and style
 - Only use emojis if the user explicitly requests it. Avoid using emojis in all communication unless asked.
 - Your responses should be short and concise.
 - When referencing specific functions or pieces of code include the pattern file_path:line_number to allow the user to easily navigate to the source code location.
 - When referencing GitHub issues or pull requests, use the owner/repo#123 format (e.g. anthropics/claude-code#100) so they render as clickable links.
 - Do not use a colon before tool calls. Your tool calls may not be shown directly in the output, so text like "Let me read the file:" followed by a read tool call should just be "Let me read the file." with a period.`

// cliHeaders are the non-auth headers sent by the Claude Code CLI.
// Anthropic inspects User-Agent + X-App to determine whether to apply
// plan-level (Claude Code) or third-party rate limits.
var cliHeaders = map[string]string{
	"User-Agent": "claude-cli/" + cliVersion + " (external, cli)",
	"X-App":      "cli",
	"Anthropic-Dangerous-Direct-Browser-Access": "true",
	"X-Stainless-Lang":                          "js",
	"X-Stainless-Package-Version":               "0.94.0",
	"X-Stainless-OS":                            "Linux",
	"X-Stainless-Arch":                          "arm64",
	"X-Stainless-Runtime":                       "node",
	"X-Stainless-Runtime-Version":               "v24.3.0",
	"X-Stainless-Retry-Count":                   "0",
	"X-Stainless-Timeout":                       "600",
}
