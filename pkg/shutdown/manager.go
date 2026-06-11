package shutdown

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type State int32

const (
	StateRunning State = iota
	StateDraining
	StateStopping
	StateStopped
)

func (s State) String() string {
	switch s {
	case StateRunning:
		return "running"
	case StateDraining:
		return "draining"
	case StateStopping:
		return "stopping"
	case StateStopped:
		return "stopped"
	default:
		return "unknown"
	}
}

type CloseFunc func(context.Context) error

type ReadyCheck func(context.Context) error

type Manager struct {
	cfg       Config
	state     atomic.Int32
	activeOps atomic.Int64
	seq       atomic.Int64

	closersMu sync.Mutex
	closers   []namedCloser

	readyMu sync.Mutex
	checks  []namedReadyCheck

	opsMu sync.Mutex
	ops   map[string]operationInfo

	logger *Logger
}

type namedCloser struct {
	name string
	fn   CloseFunc
}

type namedReadyCheck struct {
	name string
	fn   ReadyCheck
}

type operationInfo struct {
	id        string
	name      string
	method    string
	path      string
	remoteIP  string
	startedAt time.Time
	async     bool
}

func New(cfg Config) *Manager {
	m := &Manager{
		cfg:    cfg,
		ops:    make(map[string]operationInfo),
		logger: NewLogger(cfg.LogFormat, cfg.PodName),
	}
	m.state.Store(int32(StateRunning))
	m.activeOps.Store(0)
	m.logger.Event(LogEvent{Event: "module_started", State: StateRunning.String()})
	return m
}

func (m *Manager) State() State {
	return State(m.state.Load())
}

func (m *Manager) ActiveOps() int64 {
	return m.activeOps.Load()
}

func (m *Manager) RegisterCloser(name string, fn CloseFunc) {
	m.closersMu.Lock()
	defer m.closersMu.Unlock()
	m.closers = append(m.closers, namedCloser{name: name, fn: fn})
}

func (m *Manager) RegisterReadyCheck(name string, fn ReadyCheck) {
	m.readyMu.Lock()
	defer m.readyMu.Unlock()
	m.checks = append(m.checks, namedReadyCheck{name: name, fn: fn})
}

func (m *Manager) StartDraining(source string) bool {
	if m.state.CompareAndSwap(int32(StateRunning), int32(StateDraining)) {
		m.logger.Event(LogEvent{
			Event:     "shutdown_requested",
			Source:    source,
			State:     StateDraining.String(),
			ActiveOps: m.ActiveOps(),
			TimeoutMS: m.cfg.ShutdownTimeout.Milliseconds(),
			Result:    "accepted",
		})
		m.logger.Event(LogEvent{
			Event:     "readiness_changed",
			Source:    source,
			State:     StateDraining.String(),
			ActiveOps: m.ActiveOps(),
			Result:    "not_ready",
		})
		return true
	}
	m.logger.Event(LogEvent{
		Event:     "shutdown_already_started",
		Source:    source,
		State:     m.State().String(),
		ActiveOps: m.ActiveOps(),
		Result:    "ignored",
	})
	return false
}

func (m *Manager) registerOperation(info operationInfo) string {
	if info.id == "" {
		info.id = fmt.Sprintf("op-%d", m.seq.Add(1))
	}
	if info.startedAt.IsZero() {
		info.startedAt = time.Now().UTC()
	}
	m.activeOps.Add(1)
	m.opsMu.Lock()
	m.ops[info.id] = info
	m.opsMu.Unlock()
	return info.id
}

func (m *Manager) finishOperation(id string) {
	m.opsMu.Lock()
	delete(m.ops, id)
	m.opsMu.Unlock()
	m.activeOps.Add(-1)
}

func (m *Manager) operationsSnapshot() []OperationSnapshot {
	now := time.Now().UTC()
	m.opsMu.Lock()
	defer m.opsMu.Unlock()
	out := make([]OperationSnapshot, 0, len(m.ops))
	for _, op := range m.ops {
		remote := op.remoteIP
		if remote != "" {
			// Маскируем IP в демонстрационном журнале, чтобы не сохранять точный адрес клиента.
			remote = maskIP(remote)
		}
		out = append(out, OperationSnapshot{
			OpID:      op.id,
			Name:      op.name,
			Method:    op.method,
			Path:      op.path,
			RemoteIP:  remote,
			StartedAt: op.startedAt.Format(time.RFC3339Nano),
			ElapsedMS: now.Sub(op.startedAt).Milliseconds(),
			Async:     op.async,
		})
	}
	return out
}

func (m *Manager) WaitActiveOperations(ctx context.Context) error {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		active := m.ActiveOps()
		if active == 0 {
			m.logger.Event(LogEvent{Event: "active_ops_done", ActiveOps: 0, Result: "all_registered_operations_completed"})
			return nil
		}
		select {
		case <-ctx.Done():
			m.logger.Event(LogEvent{
				Level:      "warn",
				Event:      "shutdown_timeout",
				ActiveOps:  active,
				TimeoutMS:  m.cfg.ShutdownTimeout.Milliseconds(),
				Result:     "forced_by_app_policy",
				Error:      ctx.Err().Error(),
				Operations: m.operationsSnapshot(),
			})
			return ctx.Err()
		case <-ticker.C:
			m.logger.Event(LogEvent{Event: "active_ops_wait", ActiveOps: active})
		}
	}
}

func (m *Manager) CloseResources(ctx context.Context) error {
	m.state.Store(int32(StateStopping))
	m.closersMu.Lock()
	closers := append([]namedCloser(nil), m.closers...)
	m.closersMu.Unlock()

	var result error
	for _, closer := range closers {
		if err := closer.fn(ctx); err != nil {
			m.logger.Event(LogEvent{
				Level:    "error",
				Event:    "resource_close",
				Resource: closer.name,
				Result:   "error",
				Error:    err.Error(),
			})
			result = errors.Join(result, err)
			continue
		}
		m.logger.Event(LogEvent{
			Event:    "resource_close",
			Resource: closer.name,
			Result:   "ok",
		})
	}
	return result
}

func (m *Manager) Shutdown(ctx context.Context, server *http.Server, source string) error {
	m.StartDraining(source)
	waitCtx, cancel := context.WithTimeout(ctx, m.cfg.ShutdownTimeout)
	defer cancel()

	if err := server.Shutdown(waitCtx); err != nil {
		m.logger.Event(LogEvent{Level: "warn", Event: "http_server_shutdown", Resource: "http_server", Result: "error", Error: err.Error()})
	} else {
		m.logger.Event(LogEvent{Event: "http_server_shutdown", Resource: "http_server", Result: "ok"})
	}

	waitErr := m.WaitActiveOperations(waitCtx)
	closeErr := m.CloseResources(waitCtx)
	m.state.Store(int32(StateStopped))

	result := "graceful"
	if waitErr != nil || closeErr != nil {
		result = "completed_with_warnings"
	}
	m.logger.Event(LogEvent{Event: "shutdown_complete", State: StateStopped.String(), Result: result})
	return errors.Join(waitErr, closeErr)
}

func maskIP(ip string) string {
	// Достаточно для демонстрационного стенда: IPv4 10.244.0.17 -> 10.244.0.x.
	lastDot := -1
	for i := len(ip) - 1; i >= 0; i-- {
		if ip[i] == '.' {
			lastDot = i
			break
		}
	}
	if lastDot > 0 {
		return ip[:lastDot+1] + "x"
	}
	return ip
}
