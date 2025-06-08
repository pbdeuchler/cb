package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
)

// Encryptor handles credential encryption and decryption
type Encryptor struct {
	key []byte
}

// NewEncryptor creates a new encryptor with the given key
func NewEncryptor(key string) (*Encryptor, error) {
	if len(key) < 32 {
		return nil, fmt.Errorf("encryption key must be at least 32 bytes")
	}

	// Use SHA-256 to ensure we have a 32-byte key
	hash := sha256.Sum256([]byte(key))
	
	return &Encryptor{
		key: hash[:],
	}, nil
}

// EncryptCredential encrypts a plaintext credential using AES-256-GCM
func (e *Encryptor) EncryptCredential(plaintext string) (string, error) {
	if plaintext == "" {
		return "", fmt.Errorf("plaintext cannot be empty")
	}

	block, err := aes.NewCipher(e.key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	// Generate a random nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt the plaintext
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	
	// Return base64-encoded ciphertext
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// DecryptCredential decrypts a base64-encoded ciphertext using AES-256-GCM
func (e *Encryptor) DecryptCredential(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", fmt.Errorf("ciphertext cannot be empty")
	}

	// Decode base64
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64: %w", err)
	}

	block, err := aes.NewCipher(e.key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	// Extract nonce and ciphertext
	nonce, ciphertext_bytes := data[:nonceSize], data[nonceSize:]
	
	// Decrypt the ciphertext
	plaintext, err := gcm.Open(nil, nonce, ciphertext_bytes, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	return string(plaintext), nil
}

// ValidateKey checks if the encryption key is valid
func ValidateKey(key string) error {
	if len(key) < 32 {
		return fmt.Errorf("encryption key must be at least 32 bytes, got %d", len(key))
	}
	return nil
}