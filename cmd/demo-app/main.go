package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/MeDaLL1ST/k8s-grace/pkg/shutdown"
)

func main() {
	mode := getenv("APP_MODE", "module")
	cfg := shutdown.LoadConfig()

	switch mode {
	case "baseline":
		runBaseline(cfg)
	case "server-shutdown":
		runServerShutdown(cfg)
	default:
		runWithModule(cfg)
	}
}

func runWithModule(cfg shutdown.Config) {
	manager := shutdown.New(cfg)

	mux := http.NewServeMux()
	mux.HandleFunc(cfg.ReadyPath, manager.ReadyHandler())
	mux.HandleFunc(cfg.ShutdownPath, manager.ShutdownHandler())
	mux.Handle("/work", manager.Middleware(workHandler(manager)))

	// Пользовательская проверка готовности. В промышленном приложении здесь может
	// проверяться доступность БД, кэша или завершение начальной загрузки данных.
	manager.RegisterReadyCheck("demo_ready_check", shutdown.ReadyCheckOK)

	manager.RegisterCloser("demo_db", func(ctx context.Context) error {
		select {
		case <-time.After(150 * time.Millisecond):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	manager.RegisterCloser("demo_queue_client", func(ctx context.Context) error {
		select {
		case <-time.After(200 * time.Millisecond):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})

	server := &http.Server{Addr: ":8080", Handler: mux}
	shutdownDone := manager.RunSignalHandler(context.Background(), server)

	go func() {
		log.Println("demo app started in module mode on :8080")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server failed: %v", err)
		}
	}()

	if err := <-shutdownDone; err != nil {
		log.Printf("shutdown finished with warning: %v", err)
	}
}

func runBaseline(cfg shutdown.Config) {
	mux := http.NewServeMux()
	mux.HandleFunc(cfg.ReadyPath, alwaysReady)
	mux.Handle("/work", workHandler(nil))

	server := &http.Server{Addr: ":8080", Handler: mux}
	log.Println("demo app started in baseline mode on :8080")
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server failed: %v", err)
	}
}

func runServerShutdown(cfg shutdown.Config) {
	mux := http.NewServeMux()
	mux.HandleFunc(cfg.ReadyPath, alwaysReady)
	mux.Handle("/work", workHandler(nil))

	server := &http.Server{Addr: ":8080", Handler: mux}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		<-stop
		log.Println("signal received, calling http.Server.Shutdown only")
		ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			log.Printf("http server shutdown error: %v", err)
		}
	}()

	log.Println("demo app started in server-shutdown mode on :8080")
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server failed: %v", err)
	}
}

func alwaysReady(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintln(w, "ready")
}

func workHandler(manager *shutdown.Manager) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		delay := parseDuration(r.URL.Query().Get("delay"), 500*time.Millisecond)
		opID := r.URL.Query().Get("op")
		if opID == "" {
			opID = strconv.FormatInt(time.Now().UnixNano(), 10)
		}

		if r.URL.Query().Get("async") == "true" {
			if manager != nil {
				manager.TrackAsync(r.Context(), "async-"+opID, func(ctx context.Context) {
					select {
					case <-time.After(delay):
					case <-ctx.Done():
					}
				})
			} else {
				go func() { <-time.After(delay) }()
			}
			w.WriteHeader(http.StatusAccepted)
			_, _ = fmt.Fprintf(w, "async operation %s accepted\n", opID)
			return
		}

		select {
		case <-time.After(delay):
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w, "operation %s completed in %s\n", opID, delay)
		case <-r.Context().Done():
			http.Error(w, "request canceled", http.StatusRequestTimeout)
		}
	})
}

func parseDuration(raw string, def time.Duration) time.Duration {
	if raw == "" {
		return def
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return def
	}
	return value
}

func getenv(key string, def string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return def
}
