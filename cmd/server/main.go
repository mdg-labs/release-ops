// Package main is the Release Ops HTTP API and poll scheduler entrypoint.
package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/mdguggenbichler/release-ops/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	run(cfg)
}

func run(cfg *config.Config) {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	addr := cfg.GoListenAddr()
	log.Printf("release-ops server listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(fmt.Errorf("listen and serve: %w", err))
	}
}
