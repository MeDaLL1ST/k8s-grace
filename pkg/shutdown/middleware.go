package shutdown

import (
	"context"
	"net"
	"net/http"
)

func (m *Manager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m.State() != StateRunning {
			http.Error(w, m.State().String(), http.StatusServiceUnavailable)
			m.logger.Event(LogEvent{
				Level:    "warn",
				Event:    "request_rejected",
				State:    m.State().String(),
				Path:     r.URL.Path,
				Method:   r.Method,
				RemoteIP: clientIP(r),
				Result:   "503",
			})
			return
		}
		opID := r.URL.Query().Get("op")
		if opID == "" {
			opID = r.Header.Get("X-Request-ID")
		}
		opID = m.registerOperation(operationInfo{
			id:       opID,
			method:   r.Method,
			path:     r.URL.Path,
			remoteIP: clientIP(r),
			async:    false,
		})
		defer m.finishOperation(opID)
		next.ServeHTTP(w, r)
	})
}

func (m *Manager) TrackAsync(ctx context.Context, name string, fn func(context.Context)) {
	opID := m.registerOperation(operationInfo{
		id:    name,
		name:  name,
		path:  "async:" + name,
		async: true,
	})
	m.logger.Event(LogEvent{Event: "async_started", OpID: opID, ActiveOps: m.ActiveOps()})
	go func() {
		defer func() {
			m.finishOperation(opID)
			m.logger.Event(LogEvent{Event: "async_done", OpID: opID, ActiveOps: m.ActiveOps()})
		}()
		fn(ctx)
	}()
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
