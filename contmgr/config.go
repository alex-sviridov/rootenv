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
	infraNamespace  string
	imagePullSecret string
	pollInterval    time.Duration
	probeAddr       string
	metricsAddr     string
}

func loadConfig() (config, error) {
	infraNS := os.Getenv("CONTMGR_INFRA_NAMESPACE")
	if infraNS == "" {
		infraNS = "rootenv-infra"
	}
	probeAddr := os.Getenv("CONTMGR_PROBE_ADDR")
	if probeAddr == "" {
		probeAddr = ":8081"
	}
	metricsAddr := os.Getenv("CONTMGR_METRICS_ADDR")
	if metricsAddr == "" {
		metricsAddr = "0"
	}
	cfg := config{
		pbURL:           os.Getenv("CONTMGR_BACKEND_URL"),
		pbEmail:         os.Getenv("CONTMGR_BACKEND_USERNAME"),
		pbPassword:      os.Getenv("CONTMGR_BACKEND_PASSWORD"),
		infraNamespace:  infraNS,
		imagePullSecret: os.Getenv("CONTMGR_IMAGE_PULL_SECRET"),
		pollInterval:    5 * time.Second,
		probeAddr:       probeAddr,
		metricsAddr:     metricsAddr,
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
	if raw := os.Getenv("CONTMGR_POLL_INTERVAL"); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d > 0 {
			cfg.pollInterval = d
		}
	}
	return cfg, nil
}
