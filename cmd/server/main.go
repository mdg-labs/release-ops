// Package main is the Release Ops HTTP API and poll scheduler entrypoint.
package main

import (
	"fmt"
	"log"
	"net/http"
)

func main() {
	addr := "127.0.0.1:8080"
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	log.Printf("release-ops server listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(fmt.Errorf("listen and serve: %w", err))
	}
}
