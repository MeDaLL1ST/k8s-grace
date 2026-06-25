package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/MeDaLL1ST/k8s-grace/pkg/shutdown"
)

func main() {
	cfg := shutdown.LoadConfig()
	manager := shutdown.New(cfg)

	mux := http.NewServeMux()
	mux.HandleFunc(cfg.ReadyPath, manager.ReadyHandler())
	mux.HandleFunc(cfg.ShutdownPath, manager.ShutdownHandler())
	mux.Handle("/api/hello", manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintln(w, "hello")
	})))

	manager.RegisterReadyCheck("always_ok", shutdown.ReadyCheckOK)
	manager.RegisterCloser("example_resource", func(ctx context.Context) error {
		return nil
	})

	srv := &http.Server{Addr: ":8080", Handler: mux}
	done := manager.RunSignalHandler(context.Background(), srv)
	go func() { _ = srv.ListenAndServe() }()
	_ = <-done
}
