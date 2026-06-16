package config

import "testing"

func TestLoadConfig(t *testing.T) {
	t.Setenv("ATTEMPT_CONTROLLER_BACKEND_URL", "http://pb.local")
	t.Setenv("ATTEMPT_CONTROLLER_BACKEND_USERNAME", "svc_contmgr@contmgr.local")
	t.Setenv("ATTEMPT_CONTROLLER_BACKEND_PASSWORD", "secret")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.PbURL != "http://pb.local" {
		t.Errorf("pbURL = %q", cfg.PbURL)
	}
	if cfg.PbEmail != "svc_contmgr@contmgr.local" {
		t.Errorf("pbEmail = %q", cfg.PbEmail)
	}
	if cfg.PbPassword != "secret" {
		t.Errorf("pbPassword = %q", cfg.PbPassword)
	}
	if !cfg.TlsVerify {
		t.Error("tlsVerify should default to true")
	}
}

func TestLoadConfigTLSVerifyDisabled(t *testing.T) {
	t.Setenv("ATTEMPT_CONTROLLER_BACKEND_URL", "http://pb.local")
	t.Setenv("ATTEMPT_CONTROLLER_BACKEND_USERNAME", "user")
	t.Setenv("ATTEMPT_CONTROLLER_BACKEND_PASSWORD", "pass")
	t.Setenv("ATTEMPT_CONTROLLER_BACKEND_TLS_VERIFY", "false")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.TlsVerify {
		t.Error("tlsVerify should be false when env var is \"false\"")
	}
}

func TestLoadConfigMissingURL(t *testing.T) {
	t.Setenv("ATTEMPT_CONTROLLER_BACKEND_URL", "")
	t.Setenv("ATTEMPT_CONTROLLER_BACKEND_USERNAME", "user")
	t.Setenv("ATTEMPT_CONTROLLER_BACKEND_PASSWORD", "pass")

	if _, err := Load(); err == nil {
		t.Error("expected error for missing URL")
	}
}

func TestLoadConfigMissingUsername(t *testing.T) {
	t.Setenv("ATTEMPT_CONTROLLER_BACKEND_URL", "http://pb.local")
	t.Setenv("ATTEMPT_CONTROLLER_BACKEND_USERNAME", "")
	t.Setenv("ATTEMPT_CONTROLLER_BACKEND_PASSWORD", "pass")

	if _, err := Load(); err == nil {
		t.Error("expected error for missing username")
	}
}

func TestLoadConfigMissingPassword(t *testing.T) {
	t.Setenv("ATTEMPT_CONTROLLER_BACKEND_URL", "http://pb.local")
	t.Setenv("ATTEMPT_CONTROLLER_BACKEND_USERNAME", "user")
	t.Setenv("ATTEMPT_CONTROLLER_BACKEND_PASSWORD", "")

	if _, err := Load(); err == nil {
		t.Error("expected error for missing password")
	}
}
