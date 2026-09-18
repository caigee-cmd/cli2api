package server

import (
	"context"
	"net/http"
	"sync/atomic"

	"github.com/caigee-cmd/cli2api/internal/auth"
	appconsole "github.com/caigee-cmd/cli2api/internal/console"
	apigateway "github.com/caigee-cmd/cli2api/internal/gateway"
	appupdate "github.com/caigee-cmd/cli2api/internal/update"
)

// Server is the HTTP surface: CORS, OPTIONS, maintenance, mux, auth wrapping,
// and webui fallback. It does not construct Store or Manager.
type Server struct {
	Auth          auth.Verifier
	Gateway       *apigateway.Handler
	Console       *appconsole.Handler
	Update        *appupdate.Coordinator
	TouchKey      func(ctx context.Context, keyID string)
	CrossProvider *atomic.Bool

	mux *http.ServeMux
}

func New(cfg Server) *Server {
	s := cfg
	if s.mux == nil {
		s.mux = http.NewServeMux()
	}
	out := s
	out.routes()
	return &out
}

func (s *Server) updater() *appupdate.Coordinator {
	if s == nil || s.Update == nil {
		return &appupdate.Coordinator{}
	}
	return s.Update
}

func (s *Server) gatewayHandler() *apigateway.Handler {
	if s == nil || s.Gateway == nil {
		return &apigateway.Handler{}
	}
	return s.Gateway
}

func (s *Server) consoleHandler() *appconsole.Handler {
	if s == nil || s.Console == nil {
		return &appconsole.Handler{}
	}
	return s.Console
}

func (s *Server) crossProviderOn() bool {
	return s != nil && s.CrossProvider != nil && s.CrossProvider.Load()
}
