package main

import (
	"fmt"
	"os"
	"time"
)

type config struct {
	pbURL        string
	pbEmail      string
	pbPassword   string
	hostIP       string
	pollInterval time.Duration
	dockerHost   string
}

func loadConfig() (config, error) {
	cfg := config{
		pbURL:        os.Getenv("CONTMGR_BACKEND_URL"),
		pbEmail:      os.Getenv("CONTMGR_BACKEND_USERNAME"),
		pbPassword:   os.Getenv("CONTMGR_BACKEND_PASSWORD"),
		hostIP:       os.Getenv("CONTMGR_HOST_IP"),
		pollInterval: 5 * time.Second,
		dockerHost:   os.Getenv("CONTMGR_DOCKER_HOST"),
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
	if cfg.hostIP == "" {
		return config{}, fmt.Errorf("CONTMGR_HOST_IP is required")
	}
	if cfg.dockerHost == "" {
		cfg.dockerHost = "unix:///var/run/docker.sock"
	}
	if raw := os.Getenv("CONTMGR_POLL_INTERVAL"); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d > 0 {
			cfg.pollInterval = d
		}
	}
	return cfg, nil
}
