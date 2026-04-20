package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

func generateTestSigner(t *testing.T) ssh.Signer {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}
	return signer
}

func TestParseConnection_valid(t *testing.T) {
	raw := json.RawMessage(`{"host":"10.0.0.1","port":22,"user":"student"}`)
	c, err := parseConnection(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Host != "10.0.0.1" {
		t.Errorf("want host 10.0.0.1, got %s", c.Host)
	}
	if c.Port != 22 {
		t.Errorf("want port 22, got %d", c.Port)
	}
	if c.User != "student" {
		t.Errorf("want user student, got %s", c.User)
	}
}

func TestParseConnection_defaultPort(t *testing.T) {
	raw := json.RawMessage(`{"host":"10.0.0.1","user":"student"}`)
	c, err := parseConnection(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Port != 22 {
		t.Errorf("want default port 22, got %d", c.Port)
	}
}

func TestParseConnection_missingHost(t *testing.T) {
	raw := json.RawMessage(`{"user":"student"}`)
	if _, err := parseConnection(raw); err == nil {
		t.Fatal("expected error for missing host")
	}
}

func TestParseConnection_missingUser(t *testing.T) {
	raw := json.RawMessage(`{"host":"10.0.0.1"}`)
	if _, err := parseConnection(raw); err == nil {
		t.Fatal("expected error for missing user")
	}
}

func TestParseConnection_invalidJSON(t *testing.T) {
	raw := json.RawMessage(`not json`)
	if _, err := parseConnection(raw); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestLoadSigner_missingFile(t *testing.T) {
	if _, err := loadSigner("/nonexistent/key"); err == nil {
		t.Fatal("expected error for missing key file")
	}
}

func TestLoadSigner_invalidKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad_key")
	if err := os.WriteFile(path, []byte("not a valid pem key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadSigner(path); err == nil {
		t.Fatal("expected error for invalid key data")
	}
}

func TestDialSSH_refusedConnection(t *testing.T) {
	signer := generateTestSigner(t)
	conn := &serverConnection{Host: "127.0.0.1", Port: 19999, User: "nobody"}
	_, err := dialSSH(conn, signer)
	if err == nil {
		t.Fatal("expected error dialing non-existent SSH server")
	}
}
