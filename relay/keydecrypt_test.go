package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/pem"
	"testing"

	"golang.org/x/crypto/ssh"
)

// encryptPrivateKey mirrors contmgr's EncryptPrivateKey exactly.
func encryptPrivateKey(privateKeyPEM []byte, secret string) (string, error) {
	key := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, privateKeyPEM, nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func generateTestPrivateKeyPEM(t *testing.T) []byte {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	pemBlock, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	return pem.EncodeToMemory(pemBlock)
}

func TestDecryptPrivateKey_roundTrip(t *testing.T) {
	pem := generateTestPrivateKeyPEM(t)
	secret := "mysupersecretkey"

	encrypted, err := encryptPrivateKey(pem, secret)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	signer, err := decryptPrivateKey(encrypted, secret)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if signer == nil {
		t.Fatal("expected non-nil signer")
	}
}

func TestDecryptPrivateKey_signerPublicKeyMatches(t *testing.T) {
	pem := generateTestPrivateKeyPEM(t)
	secret := "anotherSecret123"

	encrypted, err := encryptPrivateKey(pem, secret)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	signer, err := decryptPrivateKey(encrypted, secret)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	// Verify the signer produces a valid signature.
	msg := []byte("test message")
	sig, err := signer.Sign(rand.Reader, msg)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if err := signer.PublicKey().Verify(msg, sig); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestDecryptPrivateKey_wrongSecret(t *testing.T) {
	pem := generateTestPrivateKeyPEM(t)
	encrypted, err := encryptPrivateKey(pem, "correctsecret")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	if _, err := decryptPrivateKey(encrypted, "wrongsecret"); err == nil {
		t.Fatal("expected error with wrong secret")
	}
}

func TestDecryptPrivateKey_invalidBase64(t *testing.T) {
	if _, err := decryptPrivateKey("not-valid-base64!!!", "secret"); err == nil {
		t.Fatal("expected error for invalid base64")
	}
}

func TestDecryptPrivateKey_tooShort(t *testing.T) {
	// A valid base64 string that decodes to fewer bytes than a GCM nonce (12 bytes).
	short := base64.StdEncoding.EncodeToString([]byte("tooshort"))
	if _, err := decryptPrivateKey(short, "secret"); err == nil {
		t.Fatal("expected error for too-short ciphertext")
	}
}

func TestDecryptPrivateKey_corruptedCiphertext(t *testing.T) {
	pem := generateTestPrivateKeyPEM(t)
	encrypted, err := encryptPrivateKey(pem, "secret")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	// Flip a byte in the ciphertext portion (after the nonce).
	data, _ := base64.StdEncoding.DecodeString(encrypted)
	data[len(data)-1] ^= 0xFF
	corrupted := base64.StdEncoding.EncodeToString(data)

	if _, err := decryptPrivateKey(corrupted, "secret"); err == nil {
		t.Fatal("expected error for corrupted ciphertext")
	}
}

func TestDecryptPrivateKey_invalidPEM(t *testing.T) {
	// Encrypt valid-looking bytes that are not a PEM private key.
	garbage := []byte("this is not a private key pem")
	encrypted, err := encryptPrivateKey(garbage, "secret")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	if _, err := decryptPrivateKey(encrypted, "secret"); err == nil {
		t.Fatal("expected error parsing non-PEM plaintext")
	}
}
