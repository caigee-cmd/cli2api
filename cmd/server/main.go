package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/caigee-cmd/cli2api/internal/api"
	"github.com/caigee-cmd/cli2api/internal/config"
	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load() // does not override existing process env
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	srv := api.New(cfg)
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	log.Printf("cli2api listening on http://%s", addr)
	if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
		log.Fatal(err)
	}
}
