package endpoint

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Endpoints struct {
	Center  string `json:"center"`
	OpenAPI string `json:"openapi"`
	Infer   string `json:"infer"`
	Source  string `json:"source"`
}

type cacheFile struct {
	Version int `json:"version"`
	Entries map[string]struct {
		Endpoint         string   `json:"endpoint"`
		InferEndpoints   []string `json:"inferEndpoints"`
		CenterEndpoint   string   `json:"centerEndpoint"`
		OpenAPIEndpoint  string   `json:"openapiEndpoint"`
	} `json:"entries"`
}

func Default() Endpoints {
	return Endpoints{
		Center:  "https://center.qoder.sh",
		OpenAPI: "https://openapi.qoder.sh",
		Infer:   "https://api1.qoder.sh",
		Source:  "builtin-default",
	}
}

func ResolveFromHome(home string) Endpoints {
	ep := Default()
	path := filepath.Join(home, ".cache", "endpoint-cache.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return ep
	}
	var c cacheFile
	if err := json.Unmarshal(b, &c); err != nil {
		return ep
	}
	entry, ok := c.Entries["prod"]
	if !ok {
		return ep
	}
	if entry.CenterEndpoint != "" {
		ep.Center = entry.CenterEndpoint
	}
	if entry.OpenAPIEndpoint != "" {
		ep.OpenAPI = entry.OpenAPIEndpoint
	}
	if entry.Endpoint != "" {
		ep.Infer = entry.Endpoint
	} else if len(entry.InferEndpoints) > 0 {
		ep.Infer = entry.InferEndpoints[0]
	}
	ep.Source = path
	return ep
}

func (e Endpoints) ChatURL() string {
	return e.Infer + "/algo/api/v2/service/pro/sse/agent_chat_generation"
}

func (e Endpoints) FinishURL() string {
	return e.Infer + "/algo/api/v2/service/business/finish"
}
