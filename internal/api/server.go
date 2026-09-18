package api

import (
	"github.com/caigee-cmd/cli2api/internal/app"
	"github.com/caigee-cmd/cli2api/internal/config"
)

// Server is the test-only compatibility facade over app.App.
// Production cmd/server constructs app.New. Do not add business here.
type Server struct {
	*app.App
}

func New(cfg config.Config) *Server {
	return &Server{App: app.New(cfg)}
}
