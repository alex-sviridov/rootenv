package config

import (
	"fmt"
	"os"
)

type config struct {
	PbURL      string
	PbEmail    string
	PbPassword string
	TlsVerify  bool
}

func Load() (config, error) {
	cfg := config{
		PbURL:      os.Getenv("ATTEMPT_CONTROLLER_BACKEND_URL"),
		PbEmail:    os.Getenv("ATTEMPT_CONTROLLER_BACKEND_USERNAME"),
		PbPassword: os.Getenv("ATTEMPT_CONTROLLER_BACKEND_PASSWORD"),
		TlsVerify:  os.Getenv("ATTEMPT_CONTROLLER_BACKEND_TLS_VERIFY") != "false",
	}
	if cfg.PbURL == "" {
		return config{}, fmt.Errorf("ATTEMPT_CONTROLLER_BACKEND_URL is required")
	}
	if cfg.PbEmail == "" {
		return config{}, fmt.Errorf("ATTEMPT_CONTROLLER_BACKEND_USERNAME is required")
	}
	if cfg.PbPassword == "" {
		return config{}, fmt.Errorf("ATTEMPT_CONTROLLER_BACKEND_PASSWORD is required")
	}
	return cfg, nil
}
