package ssh

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"fmt"

	gossh "golang.org/x/crypto/ssh"
)

// decryptPrivateKey is the inverse of contmgr's EncryptPrivateKey.
// Expects base64(nonce || ciphertext) where nonce is gcm.NonceSize() bytes.
func decryptPrivateKey(keyEncrypted, secret string) (gossh.Signer, error) {
	data, err := base64.StdEncoding.DecodeString(keyEncrypted)
	if err != nil {
		return nil, fmt.Errorf("base64 decode: %w", err)
	}

	key := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}

	signer, err := gossh.ParsePrivateKey(plaintext)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	return signer, nil
}
