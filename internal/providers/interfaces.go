package providers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/caigee-cmd/cli2api/internal/translate"
)

// ErrUnsupported is returned by capability interfaces a provider does not
// implement. Callers must treat nil adapters as missing registrations, not as
// implicit support.
var ErrUnsupported = errors.New("provider capability unsupported")

type ModelCapabilities struct {
	ContextWindow      int      `json:"context_window,omitempty"`
	ContextWindowMax   int      `json:"context_window_max,omitempty"`
	MaxOutput          int      `json:"max_output_tokens,omitempty"`
	PromptMaxTokens    int      `json:"prompt_max_tokens,omitempty"`
	MaxMode            bool     `json:"max_mode,omitempty"`
	Tools              bool     `json:"tools"`
	Images             bool     `json:"images"`
	Reasoning          bool     `json:"reasoning"`
	ReasoningOptions   []string `json:"reasoning_options,omitempty"`
	ReasoningDefault   string   `json:"reasoning_default,omitempty"`
	ReasoningType      string   `json:"reasoning_type,omitempty"`
	CanDisableThinking bool     `json:"can_disable_thinking,omitempty"`
}

type ModelInfo struct {
	NativeModel  string            `json:"native_model"`
	PublicModel  string            `json:"public_model"`
	DisplayName  string            `json:"display_name,omitempty"`
	Capabilities ModelCapabilities `json:"capabilities"`
}

// CredentialCodec validates and stores provider credentials.
type CredentialCodec interface {
	Validate(payload []byte) error
}

// LoginSession describes one browser-login round for an account.
type LoginSession struct {
	AuthURL string `json:"auth_url"`
	State   string `json:"state,omitempty"`
}

// LoginSessionProvider starts and polls provider-native browser login.
type LoginSessionProvider interface {
	StartLogin(ctx context.Context, accountID string) (LoginSession, error)
	PollLogin(ctx context.Context, accountID string) (done bool, message string, err error)
}

// LoginCompleter accepts a provider callback URL copied from the browser
// when the automatic loopback redirect cannot reach this process.
type LoginCompleter interface {
	CompleteLogin(ctx context.Context, accountID, callbackURL string) error
}

// ChatOutcome is the provider-neutral non-stream result.
type ChatOutcome struct {
	Model            string
	Content          string
	Reasoning        string
	ToolCalls        json.RawMessage
	FinishReason     string
	PromptTokens     int
	CompletionTokens int
	UsageSource      string
}

// ProviderChat executes chat for one account. Stream implementations return
// the raw upstream response for the API layer to relay.
type ProviderChat interface {
	ChatNonStream(ctx context.Context, accountID string, req translate.ChatRequest) (ChatOutcome, error)
	ChatStream(ctx context.Context, accountID string, req translate.ChatRequest) (*http.Response, error)
}

// ModelCatalogProvider lists models an account can currently serve.
type ModelCatalogProvider interface {
	Models(ctx context.Context, accountID string) ([]ModelInfo, error)
}

// ErrorClassifier maps provider errors to the internal taxonomy.
type ErrorClassifier interface {
	Classify(status int, body string) ClassifiedError
}

type ClassifiedError struct {
	Kind     string `json:"kind"`
	Status   int    `json:"status"`
	Failover bool   `json:"failover"`
	Cooldown string `json:"cooldown,omitempty"`
	Message  string `json:"message"`
}

// ImportExporter validates and exports provider-specific credential JSON.
type ImportExporter interface {
	ValidateImport(payload []byte) error
	Export(ctx context.Context, accountID string) (map[string]any, error)
}

// AccountHealth is the provider-neutral readiness snapshot for in-process
// accounts that have no child-process /health endpoint.
type AccountHealth struct {
	Ready     bool
	Hot       bool
	UID       string
	InFlight  int
	LastError string
}

// QuotaInfo is display-only usage for the accounts console. Callers must treat
// probe readiness and quota independently; quota errors never flip Ready.
type QuotaInfo struct {
	Used       float64
	Total      float64
	Remaining  float64
	Percentage float64
	Unit       string
	Exceeded   bool
	FetchedAt  string
}

// AccountProber refreshes provider-native readiness and optional display quota.
type AccountProber interface {
	Probe(ctx context.Context, accountID string) (AccountHealth, error)
	Quota(ctx context.Context, accountID string) (*QuotaInfo, error)
}

// Error is the in-process adapter error the executor classifies for
// cooldown and failover. Kind must be an accounts taxonomy value.
type Error struct {
	Kind       string
	Status     int
	Message    string
	Code       string
	Type       string
	Cooldown   time.Duration
	RetryAfter time.Duration
	Failover   *bool
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	return e.Kind
}

// Adapter bundles the optional capability interfaces. Every field may be nil;
// use Supports() before calling so unsupported paths fail explicitly.
type Adapter struct {
	ID           string
	Credential   CredentialCodec
	Login        LoginSessionProvider
	Chat         ProviderChat
	Models       ModelCatalogProvider
	Classifier   ErrorClassifier
	ImportExport ImportExporter
	Prober       AccountProber
}

func (a Adapter) Supports(capability string) bool {
	switch capability {
	case "credential":
		return a.Credential != nil
	case "login":
		return a.Login != nil
	case "chat":
		return a.Chat != nil
	case "models":
		return a.Models != nil
	case "classifier":
		return a.Classifier != nil
	case "import_export":
		return a.ImportExport != nil
	case "prober":
		return a.Prober != nil
	default:
		return false
	}
}
