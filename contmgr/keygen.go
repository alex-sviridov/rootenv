package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/pem"

	"golang.org/x/crypto/ssh"
)

func GenerateKeypair() (privateKeyPEM []byte, authorizedKeysLine []byte, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return nil, nil, err
	}
	pemBlock, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(pemBlock), ssh.MarshalAuthorizedKey(sshPub), nil
}

// EncryptPrivateKey encrypts privateKeyPEM with AES-256-GCM using SHA-256(secret) as key.
// Returns base64(nonce || ciphertext).
func EncryptPrivateKey(privateKeyPEM []byte, secret string) (string, error) {
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
