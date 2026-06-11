package shutdown

import (
	"context"
	"fmt"
	"net/http"
)

func (m *Manager) ReadyHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if m.State() != StateRunning {
			http.Error(w, "draining", http.StatusServiceUnavailable)
			return
		}
		m.readyMu.Lock()
		checks := append([]namedReadyCheck(nil), m.checks...)
		m.readyMu.Unlock()
		for _, check := range checks {
			if err := check.fn(r.Context()); err != nil {
				m.logger.Event(LogEvent{Level: "warn", Event: "ready_check_failed", Resource: check.name, Error: err.Error(), Result: "not_ready"})
				http.Error(w, "not ready: "+check.name, http.StatusServiceUnavailable)
				return
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintln(w, "ready")
	}
}

func (m *Manager) ShutdownHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if m.cfg.ShutdownToken != "" && r.Header.Get("X-Shutdown-Token") != m.cfg.ShutdownToken {
			m.logger.Event(LogEvent{Level: "warn", Event: "shutdown_unauthorized", RemoteIP: clientIP(r), Result: "403"})
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		m.StartDraining("preStop")
		w.WriteHeader(http.StatusAccepted)
		_, _ = fmt.Fprintln(w, "shutdown accepted")
	}
}

// ReadyCheckOK удобно использовать в демонстрационном приложении и примерах.
func ReadyCheckOK(context.Context) error { return nil }
