package endpoint

import (
	"net/http"
	"strings"
)

func RequestedAccount(r *http.Request) string {
	if r == nil {
		return ""
	}
	id := strings.TrimSpace(r.URL.Query().Get("account"))
	if id == "" {
		id = strings.TrimSpace(r.Header.Get("X-Qoder-Account"))
	}
	return id
}
