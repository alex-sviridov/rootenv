package main

import (
	"fmt"
	"os"
	"time"
)

type config struct {
	pbURL           string
	pbEmail         string
	pbPassword      string
	usersNamespace  string
	imagePullSecret string
	pollInterval    time.Duration
}

func loadConfig() (config, error) {
	cfg := config{
		pbURL:           os.Getenv("CONTMGR_BACKEND_URL"),
		pbEmail:         os.Getenv("CONTMGR_BACKEND_USERNAME"),
		pbPassword:      os.Getenv("CONTMGR_BACKEND_PASSWORD"),
		usersNamespace:  os.Getenv("CONTMGR_USERS_NAMESPACE"),
		imagePullSecret: os.Getenv("CONTMGR_IMAGE_PULL_SECRET"),
		pollInterval:    5 * time.Second,
	}
	if cfg.pbURL == "" {
		return config{}, fmt.Errorf("CONTMGR_BACKEND_URL is required")
	}
	if cfg.pbEmail == "" {
		return config{}, fmt.Errorf("CONTMGR_BACKEND_USERNAME is required")
	}
	if cfg.pbPassword == "" {
		return config{}, fmt.Errorf("CONTMGR_BACKEND_PASSWORD is required")
	}
	if cfg.usersNamespace == "" {
		return config{}, fmt.Errorf("CONTMGR_USERS_NAMESPACE is required")
	}
	if raw := os.Getenv("CONTMGR_POLL_INTERVAL"); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d > 0 {
			cfg.pollInterval = d
		}
	}
	return cfg, nil
}
