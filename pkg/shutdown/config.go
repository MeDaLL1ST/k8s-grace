package shutdown

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	ShutdownTimeout time.Duration
	LogFormat       string
	ReadyPath       string
	ShutdownPath    string
	ShutdownToken   string
	ServiceName     string
	PodName         string
}

func LoadConfig() Config {
	timeoutSeconds := getenvInt("SHUTDOWN_TIMEOUT_SECONDS", 25)
	return Config{
		ShutdownTimeout: time.Duration(timeoutSeconds) * time.Second,
		LogFormat:       getenv("LOG_FORMAT", "json"),
		ReadyPath:       getenv("READY_PATH", "/ready"),
		ShutdownPath:    getenv("SHUTDOWN_PATH", "/shutdown"),
		ShutdownToken:   getenv("SHUTDOWN_TOKEN", ""),
		ServiceName:     getenv("SERVICE_NAME", "graceful-app"),
		PodName:         getenv("HOSTNAME", "local"),
	}
}

func getenv(key string, def string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return def
}

func getenvInt(key string, def int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return def
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return def
	}
	return value
}
