package providers

import (
	"fmt"
	"strings"
)

type RuntimeKind string

const (
	RuntimeChildProcess RuntimeKind = "child_process"
	RuntimeInProcess    RuntimeKind = "in_process"
)

type AuthType string

const (
	AuthNone   AuthType = "none"
	AuthOAuth  AuthType = "oauth"
	AuthPAT    AuthType = "pat"
	AuthNative AuthType = "native"
)

type ProviderCapabilities struct {
	Chat         bool `json:"chat"`
	Stream       bool `json:"stream"`
	Tools        bool `json:"tools"`
	Images       bool `json:"images"`
	Reasoning    bool `json:"reasoning"`
	ModelCatalog bool `json:"model_catalog"`
	Usage        bool `json:"usage"`
	Login        bool `json:"login"`
	BrowserLogin bool `json:"browser_login"`
	PATLogin     bool `json:"pat_login"`
	ImportExport bool `json:"import_export"`
}

type RegionDescriptor struct {
	ID            string         `json:"id"`
	Label         string         `json:"label"`
	ChatBase      string         `json:"chat_base"`
	BillingBase   string         `json:"billing_base"`
	AuthBase      string         `json:"auth_base"`
	DefaultDomain string         `json:"default_domain"`
	Checkin       *CheckinPolicy `json:"checkin,omitempty"`
}

type ProviderDescriptor struct {
	ID                string               `json:"id"`
	Label             string               `json:"label"`
	Runtime           RuntimeKind          `json:"runtime"`
	AuthTypes         []AuthType           `json:"auth_types"`
	CredentialFormats []string             `json:"credential_formats"`
	Capabilities      ProviderCapabilities `json:"capabilities"`
	Regions           []RegionDescriptor   `json:"regions"`
	DefaultRegion     string               `json:"default_region"`
}

func (d ProviderDescriptor) Region(id string) (RegionDescriptor, bool) {
	for _, region := range d.Regions {
		if region.ID == id {
			return region, true
		}
	}
	return RegionDescriptor{}, false
}

func (d ProviderDescriptor) SupportsCredentialFormat(format string) bool {
	for _, f := range d.CredentialFormats {
		if f == format {
			return true
		}
	}
	return false
}

func (d ProviderDescriptor) SupportsAuthType(auth AuthType) bool {
	for _, a := range d.AuthTypes {
		if a == auth {
			return true
		}
	}
	return false
}

// Qoder descriptor. Region selects the pinned CLI and config directory:
// global uses @qoder-ai/qodercli + .qoder, cn uses @qodercn-ai/qoderclicn + .qoder-cn.
var Qoder = ProviderDescriptor{
	ID:                "qoder",
	Label:             "Qoder",
	Runtime:           RuntimeChildProcess,
	AuthTypes:         []AuthType{AuthNone, AuthOAuth, AuthPAT, AuthNative},
	CredentialFormats: []string{"qoder-pat-v1", "qoder-native-v1"},
	Capabilities: ProviderCapabilities{
		Chat: true, Stream: true, Tools: true, Images: true, Reasoning: true,
		ModelCatalog: true, Usage: true, Login: true, BrowserLogin: true,
		PATLogin: true, ImportExport: true,
	},
	Regions: []RegionDescriptor{
		{
			ID: "global", Label: "Global", ChatBase: "https://api2.qoder.sh",
			BillingBase: "https://openapi.qoder.sh", AuthBase: "https://qoder.sh",
			DefaultDomain: "qoder.global",
			Checkin: &CheckinPolicy{
				Timezone: "Asia/Shanghai",
			},
		},
		{
			ID: "cn", Label: "CN", ChatBase: "https://gateway.qoder.com.cn",
			BillingBase: "https://openapi.qoder.com.cn", AuthBase: "https://qoder.com.cn",
			DefaultDomain: "qoder.com.cn",
			Checkin: &CheckinPolicy{
				Timezone: "Asia/Shanghai",
			},
		},
	},
	DefaultRegion: "global",
}

// WorkBuddy descriptor. Protocol constants stay in internal/providers/workbuddy;
// the registry only carries control-plane metadata.
var WorkBuddy = ProviderDescriptor{
	ID:                "workbuddy",
	Label:             "WorkBuddy",
	Runtime:           RuntimeInProcess,
	AuthTypes:         []AuthType{AuthNone, AuthOAuth},
	CredentialFormats: []string{"workbuddy-oauth-v1", "workbuddy-accounts"},
	Capabilities: ProviderCapabilities{
		Chat: true, Stream: true, Tools: true, Images: false, Reasoning: true,
		ModelCatalog: true, Usage: true, Login: true, BrowserLogin: true,
		PATLogin: false, ImportExport: true,
	},
	Regions: []RegionDescriptor{
		{
			ID: "cn", Label: "CN", ChatBase: "https://www.workbuddy.cn",
			BillingBase: "https://www.workbuddy.cn", AuthBase: "https://www.workbuddy.cn",
			DefaultDomain: "codebuddy.cn",
			Checkin: &CheckinPolicy{
				Timezone: "Asia/Shanghai",
			},
		},
		{
			ID: "global", Label: "Global", ChatBase: "https://www.workbuddy.ai",
			BillingBase: "https://www.workbuddy.ai", AuthBase: "https://www.workbuddy.ai",
			DefaultDomain: "workbuddy.ai",
		},
	},
	DefaultRegion: "cn",
}

// Trae descriptor. Protocol constants stay in internal/providers/trae;
// the registry only carries control-plane metadata. CN Solo is the only
// region implemented; do not spawn official traecli.
var Trae = ProviderDescriptor{
	ID:                "trae",
	Label:             "Trae",
	Runtime:           RuntimeInProcess,
	AuthTypes:         []AuthType{AuthOAuth},
	CredentialFormats: []string{"trae-oauth-v1"},
	Capabilities: ProviderCapabilities{
		Chat: true, Stream: true, Tools: true, Images: false, Reasoning: true,
		ModelCatalog: true, Usage: true, Login: true, BrowserLogin: true,
		PATLogin: false, ImportExport: true,
	},
	Regions: []RegionDescriptor{
		{
			ID: "cn", Label: "CN Solo", ChatBase: "https://trae-api-cn.mchost.guru",
			BillingBase: "https://api.trae.cn", AuthBase: "https://api.trae.com.cn",
			DefaultDomain: "trae.cn",
			Checkin: &CheckinPolicy{
				Timezone: "Asia/Shanghai",
			},
		},
	},
	DefaultRegion: "cn",
}

// Devin descriptor. Protocol constants stay in internal/providers/devin.
// Global-only Connect-RPC adapter; no Devin CLI child process.
var Devin = ProviderDescriptor{
	ID:                "devin",
	Label:             "Devin",
	Runtime:           RuntimeInProcess,
	AuthTypes:         []AuthType{AuthOAuth},
	CredentialFormats: []string{"devin-session-v1"},
	Capabilities: ProviderCapabilities{
		Chat: true, Stream: true, Tools: true, Images: true, Reasoning: true,
		ModelCatalog: true, Usage: true, Login: true, BrowserLogin: true,
		PATLogin: false, ImportExport: true,
	},
	Regions: []RegionDescriptor{
		{
			ID: "global", Label: "Global", ChatBase: "https://server.codeium.com",
			BillingBase: "https://api.devin.ai", AuthBase: "https://app.devin.ai",
			DefaultDomain: "devin.ai",
		},
	},
	DefaultRegion: "global",
}

// Command descriptor. Protocol constants stay in internal/providers/command.
// Global-only in-process adapter. Auth is a single pasted user_… Bearer key
// shared by the CLI and the API: no OAuth, no browser loopback. Generation goes
// through /alpha/generate, the CLI's own endpoint, so the $1 Go plan (which has
// no /provider/v1/* API access) is served too.
var Command = ProviderDescriptor{
	ID:                "command",
	Label:             "Command Code",
	Runtime:           RuntimeInProcess,
	AuthTypes:         []AuthType{AuthPAT},
	CredentialFormats: []string{"command-key-v1"},
	Capabilities: ProviderCapabilities{
		Chat: true, Stream: true, Tools: true, Images: true, Reasoning: false,
		ModelCatalog: true, Usage: true, Login: false, BrowserLogin: false,
		PATLogin: true, ImportExport: true,
	},
	Regions: []RegionDescriptor{
		{
			ID: "global", Label: "Global", ChatBase: "https://api.commandcode.ai",
			BillingBase: "https://api.commandcode.ai", AuthBase: "https://api.commandcode.ai",
			DefaultDomain: "commandcode.ai",
		},
	},
	DefaultRegion: "global",
}

// Codex descriptor. OpenAI Codex (ChatGPT subscription) via the ChatGPT
// backend Responses endpoint; OAuth with a fixed loopback redirect.
var Codex = ProviderDescriptor{
	ID:                "codex",
	Label:             "Codex",
	Runtime:           RuntimeInProcess,
	AuthTypes:         []AuthType{AuthOAuth},
	CredentialFormats: []string{"codex-oauth-v1"},
	Capabilities: ProviderCapabilities{
		Chat: true, Stream: true, Tools: true, Images: true, Reasoning: true,
		ModelCatalog: true, Usage: true, Login: true, BrowserLogin: true,
		PATLogin: false, ImportExport: true,
	},
	Regions: []RegionDescriptor{
		{
			ID: "global", Label: "Global", ChatBase: "https://chatgpt.com/backend-api/codex",
			AuthBase: "https://auth.openai.com", BillingBase: "https://chatgpt.com",
			DefaultDomain: "chatgpt.com",
		},
	},
	DefaultRegion: "global",
}

// Zhipu descriptor. Protocol constants stay in internal/providers/zhipu.
// CN-only in-process adapter. Auth is a pasted API key; pay-as-you-go and
// Coding Plan differ by base URL, not by a second provider family.
var Zhipu = ProviderDescriptor{
	ID:                "zhipu",
	Label:             "Zhipu GLM",
	Runtime:           RuntimeInProcess,
	AuthTypes:         []AuthType{AuthPAT},
	CredentialFormats: []string{"zhipu-key-v1"},
	Capabilities: ProviderCapabilities{
		Chat: true, Stream: true, Tools: true, Images: false, Reasoning: true,
		ModelCatalog: true, Usage: true, Login: false, BrowserLogin: false,
		PATLogin: true, ImportExport: true,
	},
	Regions: []RegionDescriptor{
		{
			ID: "cn", Label: "CN", ChatBase: "https://open.bigmodel.cn",
			BillingBase: "https://open.bigmodel.cn", AuthBase: "https://open.bigmodel.cn",
			DefaultDomain: "bigmodel.cn",
		},
	},
	DefaultRegion: "cn",
}

// QwenWork descriptor. Protocol constants stay in internal/providers/qwenwork;
// the registry only carries control-plane metadata. QwenWork is the rebranded
// QoderWork desktop product: global is qwenwork.ai, cn is 千问办公 qwenwork.cn.
// Daily free credits are granted by the server at 00:00 Asia/Shanghai (CN
// wallet copy: "每日 0 点刷新"; global pricing lists the same "daily login
// bonus"), so both regions use that day boundary.
var QwenWork = ProviderDescriptor{
	ID:                "qwenwork",
	Label:             "QwenWork",
	Runtime:           RuntimeInProcess,
	AuthTypes:         []AuthType{AuthOAuth},
	CredentialFormats: []string{"qwenwork-oauth-v1"},
	Capabilities: ProviderCapabilities{
		Chat: true, Stream: true, Tools: true, Images: true, Reasoning: true,
		ModelCatalog: true, Usage: true, Login: true, BrowserLogin: true,
		PATLogin: false, ImportExport: true,
	},
	Regions: []RegionDescriptor{
		{
			ID: "global", Label: "International", ChatBase: "https://gateway.qwenwork.ai",
			BillingBase: "https://gateway.qwenwork.ai", AuthBase: "https://gateway.qwenwork.ai",
			DefaultDomain: "qwenwork.ai",
			Checkin: &CheckinPolicy{
				Timezone: "Asia/Shanghai",
			},
		},
		{
			ID: "cn", Label: "CN", ChatBase: "https://gateway.qwenwork.cn",
			BillingBase: "https://gateway.qwenwork.cn", AuthBase: "https://gateway.qwenwork.cn",
			DefaultDomain: "qwenwork.cn",
			Checkin: &CheckinPolicy{
				Timezone: "Asia/Shanghai",
			},
		},
	},
	DefaultRegion: "global",
}

// Kimi descriptor. Protocol constants stay in internal/providers/kimi.
// CN-only in-process adapter for the Kimi Code subscription: OAuth device grant
// against auth.kimi.com, distinct from the Moonshot Open Platform API-key
// product that lives on api.moonshot.cn.
var Kimi = ProviderDescriptor{
	ID:                "kimi",
	Label:             "Kimi Code",
	Runtime:           RuntimeInProcess,
	AuthTypes:         []AuthType{AuthOAuth},
	CredentialFormats: []string{"kimi-oauth-v1"},
	Capabilities: ProviderCapabilities{
		Chat: true, Stream: true, Tools: true, Images: true, Reasoning: true,
		ModelCatalog: true, Usage: true, Login: true, BrowserLogin: true,
		PATLogin: false, ImportExport: true,
	},
	Regions: []RegionDescriptor{
		{
			ID: "cn", Label: "CN", ChatBase: "https://api.kimi.com/coding/v1",
			BillingBase: "https://www.kimi.com", AuthBase: "https://auth.kimi.com",
			DefaultDomain: "kimi.com",
		},
	},
	DefaultRegion: "cn",
}

// CodeArts descriptor. Protocol constants stay in internal/providers/codearts.
// CN-only in-process adapter for Huawei Cloud CodeArts Agent (码道): OAuth PKCE
// against the CodeArts portal mints a short-lived STS trio, and chat goes
// directly to the snap-access OpenAI-compatible gateway. The AK/SK in the
// credential are the STS material, not the account-level IAM keys.
var CodeArts = ProviderDescriptor{
	ID:                "codearts",
	Label:             "CodeArts",
	Runtime:           RuntimeInProcess,
	AuthTypes:         []AuthType{AuthOAuth},
	CredentialFormats: []string{"codearts-oauth-v1"},
	Capabilities: ProviderCapabilities{
		Chat: true, Stream: true, Tools: true, Images: true, Reasoning: true,
		ModelCatalog: true, Usage: true, Login: true, BrowserLogin: true,
		PATLogin: false, ImportExport: true,
	},
	Regions: []RegionDescriptor{
		{
			ID: "cn-north-4", Label: "CN",
			ChatBase:      "https://snap-access.cn-north-4.myhuaweicloud.com",
			BillingBase:   "https://snap-access.cn-north-4.myhuaweicloud.com",
			AuthBase:      "https://codearts.huaweicloud.com",
			DefaultDomain: "huaweicloud.com",
			Checkin: &CheckinPolicy{
				Timezone: "Asia/Shanghai",
			},
		},
	},
	DefaultRegion: "cn-north-4",
}

// Kiro descriptor. Protocol constants stay in internal/providers/kiro.
// In-process adapter for a personal Kiro login. Chat goes to CreateResponse
// on runtime.{region}.kiro.dev. The two regions are the data planes the IDE uses.
var Kiro = ProviderDescriptor{
	ID:                "kiro",
	Label:             "Kiro",
	Runtime:           RuntimeInProcess,
	AuthTypes:         []AuthType{AuthOAuth, AuthPAT},
	CredentialFormats: []string{"kiro-oauth-v1"},
	Capabilities: ProviderCapabilities{
		Chat: true, Stream: true, Tools: true, Images: true, Reasoning: true,
		ModelCatalog: true, Usage: false, Login: true, BrowserLogin: true,
		PATLogin: true, ImportExport: true,
	},
	Regions: []RegionDescriptor{
		{
			ID: "us-east-1", Label: "US East",
			ChatBase: "https://runtime.us-east-1.kiro.dev", BillingBase: "https://q.us-east-1.amazonaws.com",
			AuthBase: "https://oidc.us-east-1.amazonaws.com", DefaultDomain: "kiro.dev",
		},
		{
			ID: "eu-central-1", Label: "EU Central",
			ChatBase: "https://runtime.eu-central-1.kiro.dev", BillingBase: "https://q.eu-central-1.amazonaws.com",
			AuthBase: "https://oidc.eu-central-1.amazonaws.com", DefaultDomain: "kiro.dev",
		},
	},
	DefaultRegion: "us-east-1",
}

// Meta descriptor. Protocol constants stay in internal/providers/meta.
// Global-only Muse (Meta) adapter: RFC 8628 device grant against auth.meta.com,
// minting an LLM API key at api.meta.ai/muse-code/key. Alias "muse" resolves to
// this family at NormalizeProviderFamily / Get; it is not a second provider.
var Meta = ProviderDescriptor{
	ID:                "meta",
	Label:             "Muse (Meta)",
	Runtime:           RuntimeInProcess,
	AuthTypes:         []AuthType{AuthOAuth, AuthPAT},
	CredentialFormats: []string{"meta-oauth-v1", "meta-key-v1"},
	Capabilities: ProviderCapabilities{
		Chat: true, Stream: true, Tools: true, Images: true, Reasoning: true,
		ModelCatalog: true, Usage: false, Login: true, BrowserLogin: true,
		PATLogin: true, ImportExport: true,
	},
	Regions: []RegionDescriptor{
		{
			ID: "global", Label: "Global", ChatBase: "https://api.meta.ai/v1",
			BillingBase: "https://www.meta.ai", AuthBase: "https://auth.meta.com",
			DefaultDomain: "meta.ai",
		},
	},
	DefaultRegion: "global",
}

// MiniMaxCode is the MiniMax Code subscription product, reached through its
// OAuth device flow and Anthropic-compatible agent gateway. It is distinct
// from the MiniMax Open Platform API-key gateway.
var MiniMaxCode = ProviderDescriptor{
	ID:                "minimaxcode",
	Label:             "MiniMax Code",
	Runtime:           RuntimeInProcess,
	AuthTypes:         []AuthType{AuthOAuth},
	CredentialFormats: []string{"minimaxcode-oauth-v1"},
	Capabilities: ProviderCapabilities{
		Chat: true, Stream: true, Tools: true, Images: true, Reasoning: false,
		ModelCatalog: true, Usage: true, Login: true, BrowserLogin: true,
		PATLogin: false, ImportExport: true,
	},
	Regions: []RegionDescriptor{
		{
			ID: "cn", Label: "CN", ChatBase: "https://agent.minimax.cn",
			BillingBase: "https://agent.minimax.cn", AuthBase: "https://account.minimax.cn",
			DefaultDomain: "minimax.cn",
			Checkin: &CheckinPolicy{
				Timezone: "Asia/Shanghai",
			},
		},
		{
			ID: "global", Label: "Global", ChatBase: "https://agent.minimax.io",
			BillingBase: "https://agent.minimax.io", AuthBase: "https://account.minimax.io",
			DefaultDomain: "minimax.io",
			Checkin: &CheckinPolicy{
				Timezone: "Asia/Shanghai",
			},
		},
	},
	DefaultRegion: "cn",
}

var registry = map[string]ProviderDescriptor{
	Qoder.ID:       Qoder,
	WorkBuddy.ID:   WorkBuddy,
	Trae.ID:        Trae,
	Devin.ID:       Devin,
	Command.ID:     Command,
	Codex.ID:       Codex,
	Zhipu.ID:       Zhipu,
	QwenWork.ID:    QwenWork,
	Kimi.ID:        Kimi,
	CodeArts.ID:    CodeArts,
	Kiro.ID:        Kiro,
	Meta.ID:        Meta,
	MiniMaxCode.ID: MiniMaxCode,
}

func Get(id string) (ProviderDescriptor, bool) {
	key := strings.TrimSpace(strings.ToLower(id))
	if key == "muse" {
		key = "meta"
	}
	d, ok := registry[key]
	return d, ok
}

func List() []ProviderDescriptor {
	return []ProviderDescriptor{Qoder, WorkBuddy, Trae, Devin, Command, Codex, Zhipu, QwenWork, Kimi, CodeArts, Kiro, Meta, MiniMaxCode}
}

// Resolve validates a provider/region pair. Empty values fall back to the
// historical qoder/global default so pre-provider rows keep behaving the same.
func Resolve(provider, region string) (ProviderDescriptor, RegionDescriptor, error) {
	p := strings.TrimSpace(strings.ToLower(provider))
	r := strings.TrimSpace(strings.ToLower(region))
	if p == "" {
		p = Qoder.ID
	}
	descriptor, ok := Get(p)
	if !ok {
		return ProviderDescriptor{}, RegionDescriptor{}, fmt.Errorf("unknown provider %q", provider)
	}
	if r == "" {
		r = descriptor.DefaultRegion
	}
	regionDesc, ok := descriptor.Region(r)
	if !ok {
		return ProviderDescriptor{}, RegionDescriptor{}, fmt.Errorf("unknown region %q for provider %q", region, descriptor.ID)
	}
	return descriptor, regionDesc, nil
}

// ValidateCredentialFormat rejects formats that the provider cannot store.
func ValidateCredentialFormat(provider, format string) error {
	descriptor, _, err := Resolve(provider, "")
	if err != nil {
		return err
	}
	if !descriptor.SupportsCredentialFormat(format) {
		return fmt.Errorf("credential format %q is not supported by provider %q", format, descriptor.ID)
	}
	return nil
}
