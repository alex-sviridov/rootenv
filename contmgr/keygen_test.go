package main

import (
	"strings"
	"testing"
)

func TestGenerateKeypair(t *testing.T) {
	priv, pub, err := GenerateKeypair()
	if err != nil {
		t.Fatalf("GenerateKeypair: %v", err)
	}
	if len(priv) == 0 {
		t.Error("empty private key PEM")
	}
	if !strings.HasPrefix(string(pub), "ssh-ed25519 ") {
		t.Errorf("pub key should start with ssh-ed25519, got: %s", pub)
	}
}

func TestEncryptPrivateKeyRoundtrip(t *testing.T) {
	priv, _, err := GenerateKeypair()
	if err != nil {
		t.Fatal(err)
	}
	const secret = "secretsecretsecretsecretsecretsec"
	ct1, err := EncryptPrivateKey(priv, secret)
	if err != nil {
		t.Fatalf("EncryptPrivateKey: %v", err)
	}
	if ct1 == "" {
		t.Error("empty ciphertext")
	}
	// Each call uses a fresh random nonce so ciphertexts must differ.
	ct2, err := EncryptPrivateKey(priv, secret)
	if err != nil {
		t.Fatal(err)
	}
	if ct1 == ct2 {
		t.Error("expected different ciphertext each call (random nonce)")
	}
}
