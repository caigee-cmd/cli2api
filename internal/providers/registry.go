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
	ID            string `json:"id"`
	Label         string `json:"label"`
	ChatBase      string `json:"chat_base"`
	BillingBase   string `json:"billing_base"`
	AuthBase      string `json:"auth_base"`
	DefaultDomain string `json:"default_domain"`
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
	CredentialFormats: []string{"qoder-native-v1"},
	Capabilities: ProviderCapabilities{
		Chat: true, Stream: true, Tools: true, Images: true, Reasoning: true,
		ModelCatalog: true, Usage: true, Login: true, BrowserLogin: true,
		PATLogin: true, ImportExport: true,
	},
	Regions: []RegionDescriptor{
		{
			ID: "global", Label: "Global", ChatBase: "https://api1.qoder.sh",
			BillingBase: "https://openapi.qoder.sh", AuthBase: "https://qoder.sh",
			DefaultDomain: "qoder.global",
		},
		{
			ID: "cn", Label: "CN", ChatBase: "https://gateway.qoder.com.cn",
			BillingBase: "https://openapi.qoder.com.cn", AuthBase: "https://qoder.com.cn",
			DefaultDomain: "qoder.com.cn",
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
	CredentialFormats: []string{"workbuddy-oauth-v1"},
	Capabilities: ProviderCapabilities{
		Chat: true, Stream: true, Tools: true, Images: false, Reasoning: true,
		ModelCatalog: true, Usage: true, Login: true, BrowserLogin: true,
		PATLogin: false, ImportExport: true,
	},
	Regions: []RegionDescriptor{
		{
			ID: "cn", Label: "CN", ChatBase: "https://copilot.tencent.com",
			BillingBase: "https://www.codebuddy.cn", AuthBase: "https://copilot.tencent.com",
			DefaultDomain: "codebuddy.cn",
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
		},
	},
	DefaultRegion: "cn",
}

var registry = map[string]ProviderDescriptor{
	Qoder.ID:     Qoder,
	WorkBuddy.ID: WorkBuddy,
	Trae.ID:      Trae,
}

func Get(id string) (ProviderDescriptor, bool) {
	d, ok := registry[strings.TrimSpace(strings.ToLower(id))]
	return d, ok
}

func List() []ProviderDescriptor {
	return []ProviderDescriptor{Qoder, WorkBuddy, Trae}
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
