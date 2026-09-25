package command

// Pinned upstream constants. Command Code's /alpha/generate is an undocumented,
// CLI-coupled endpoint: the gateway keys on x-command-code-version, so a stale
// value risks rejection. Keep PinnedCLIVersion in step with the published
// `command-code` npm package and fail loudly (see errors.go) rather than
// silently degrading.
const (
	// CredentialFormat is the canonical stored credential format for
	// provider=command.
	CredentialFormat = "command-key-v1"

	// BaseURL is the Command Code API origin. The CLI also ships --local /
	// --staging origins; this adapter only speaks production.
	BaseURL = "https://api.commandcode.ai"

	// PathModels is the (anonymous) model catalog. It is readable without a key
	// and carries each model's context_length + supported_endpoints.
	PathModels = "/provider/v1/models"

	// PathGenerate is the CLI's own generation endpoint. It is not plan-gated,
	// so it serves every plan; on the $1 Go plan it is the only path, since
	// /provider/v1/messages and /provider/v1/chat/completions return
	// 403 upgrade_required there.
	PathGenerate = "/alpha/generate"

	// PathWhoami validates a key. 200 => key resolves.
	PathWhoami = "/alpha/whoami"

	// PathBillingCredits reports the account credit balance.
	PathBillingCredits = "/alpha/billing/credits"

	// KeyPrefix is the shape of a Command Code key ("user_…").
	KeyPrefix = "user_"

	// PinnedCLIVersion is sent as x-command-code-version on /alpha/generate.
	// Verified against the published `command-code` npm package. Override at
	// runtime with CMD_CLI_VERSION when the gateway moves ahead of this build.
	PinnedCLIVersion = "1.65.0"

	// CLIEnvironment is the x-cli-environment header value.
	CLIEnvironment = "production"

	// DefaultMaxTokens caps output when the client sends no max_tokens. The
	// gateway clamps anything larger to the model's true ceiling.
	DefaultMaxTokens = 32000

	// QuotaUnit labels credit-denominated usage windows.
	QuotaUnit = "credits"

	// VersionEnv overrides PinnedCLIVersion without a rebuild.
	VersionEnv = "CMD_CLI_VERSION"
)
