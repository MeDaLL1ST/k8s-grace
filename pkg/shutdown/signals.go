package shutdown

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

func (m *Manager) RunSignalHandler(ctx context.Context, server *http.Server) <-chan error {
	done := make(chan error, 1)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		defer signal.Stop(stop)
		select {
		case sig := <-stop:
			m.logger.Event(LogEvent{Event: "signal_received", Source: sig.String(), ActiveOps: m.ActiveOps()})
			done <- m.Shutdown(ctx, server, sig.String())
		case <-ctx.Done():
			done <- ctx.Err()
		}
	}()
	return done
}
