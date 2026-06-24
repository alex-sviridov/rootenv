package main

import (
	"log/slog"
	"testing"
)

func TestLoadConfig_logLevel_default(t *testing.T) {
	t.Setenv("RELAY_MY_NAMESPACE", "ns")
	t.Setenv("RELAY_SKIP_AUTH", "true")
	t.Setenv("LOG_LEVEL", "")

	cfg, ok := loadConfig()
	if !ok {
		t.Fatal("loadConfig returned false")
	}
	if cfg.logLevel != slog.LevelInfo {
		t.Errorf("default log level: got %v, want %v", cfg.logLevel, slog.LevelInfo)
	}
}

func TestLoadConfig_logLevel_debug(t *testing.T) {
	t.Setenv("RELAY_MY_NAMESPACE", "ns")
	t.Setenv("RELAY_SKIP_AUTH", "true")
	t.Setenv("LOG_LEVEL", "debug")

	cfg, ok := loadConfig()
	if !ok {
		t.Fatal("loadConfig returned false")
	}
	if cfg.logLevel != slog.LevelDebug {
		t.Errorf("debug log level: got %v, want %v", cfg.logLevel, slog.LevelDebug)
	}
}
