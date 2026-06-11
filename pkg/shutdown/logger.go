package shutdown

import (
	"encoding/json"
	"fmt"
	"log"
	"time"
)

type OperationSnapshot struct {
	OpID      string `json:"op_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Method    string `json:"method,omitempty"`
	Path      string `json:"path,omitempty"`
	RemoteIP  string `json:"remote_ip,omitempty"`
	StartedAt string `json:"started_at,omitempty"`
	ElapsedMS int64  `json:"elapsed_ms,omitempty"`
	Async     bool   `json:"async,omitempty"`
}

type LogEvent struct {
	TS         string              `json:"ts"`
	Level      string              `json:"level"`
	Event      string              `json:"event"`
	Source     string              `json:"source,omitempty"`
	Pod        string              `json:"pod,omitempty"`
	State      string              `json:"state,omitempty"`
	OpID       string              `json:"op_id,omitempty"`
	Path       string              `json:"path,omitempty"`
	Method     string              `json:"method,omitempty"`
	RemoteIP   string              `json:"remote_ip,omitempty"`
	ActiveOps  int64               `json:"active_ops,omitempty"`
	TimeoutMS  int64               `json:"timeout_ms,omitempty"`
	Resource   string              `json:"resource,omitempty"`
	Result     string              `json:"result,omitempty"`
	Error      string              `json:"error,omitempty"`
	Operations []OperationSnapshot `json:"operations,omitempty"`
}

type Logger struct {
	format string
	pod    string
}

func NewLogger(format string, pod string) *Logger {
	return &Logger{format: format, pod: pod}
}

func (l *Logger) Event(event LogEvent) {
	if event.TS == "" {
		event.TS = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if event.Level == "" {
		event.Level = "info"
	}
	if event.Pod == "" {
		event.Pod = l.pod
	}
	if l.format == "text" {
		log.Printf("level=%s event=%s source=%s state=%s active_ops=%d result=%s error=%s",
			event.Level, event.Event, event.Source, event.State, event.ActiveOps, event.Result, event.Error)
		return
	}
	payload, err := json.Marshal(event)
	if err != nil {
		fmt.Printf("{\"level\":\"error\",\"event\":\"log_marshal_failed\",\"error\":%q}\n", err.Error())
		return
	}
	fmt.Println(string(payload))
}
