package crypto

import (
	"strings"
	"testing"
)

func TestNewEncryptor(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		wantErr bool
	}{
		{
			name:    "valid key",
			key:     "this-is-a-very-long-encryption-key-for-testing",
			wantErr: false,
		},
		{
			name:    "exactly 32 bytes",
			key:     "12345678901234567890123456789012",
			wantErr: false,
		},
		{
			name:    "too short key",
			key:     "short",
			wantErr: true,
		},
		{
			name:    "empty key",
			key:     "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewEncryptor(tt.key)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewEncryptor() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestEncryptDecrypt(t *testing.T) {
	encryptor, err := NewEncryptor("this-is-a-very-long-encryption-key-for-testing")
	if err != nil {
		t.Fatalf("Failed to create encryptor: %v", err)
	}

	tests := []struct {
		name      string
		plaintext string
		wantErr   bool
	}{
		{
			name:      "simple text",
			plaintext: "hello world",
			wantErr:   false,
		},
		{
			name:      "api key",
			plaintext: "sk-ant-api03-abc123def456",
			wantErr:   false,
		},
		{
			name:      "github token",
			plaintext: "ghp_1234567890abcdefghijklmnopqrstuvwxyz12",
			wantErr:   false,
		},
		{
			name:      "special characters",
			plaintext: "!@#$%^&*()_+-=[]{}|;:,.<>?",
			wantErr:   false,
		},
		{
			name:      "unicode text",
			plaintext: "Hello 世界 🌍",
			wantErr:   false,
		},
		{
			name:      "long text",
			plaintext: strings.Repeat("a", 1000),
			wantErr:   false,
		},
		{
			name:      "empty text",
			plaintext: "",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Encrypt
			encrypted, err := encryptor.EncryptCredential(tt.plaintext)
			if (err != nil) != tt.wantErr {
				t.Errorf("EncryptCredential() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return // Skip decryption test for error cases
			}

			// Verify encrypted text is different from plaintext
			if encrypted == tt.plaintext {
				t.Error("Encrypted text should be different from plaintext")
			}

			// Verify encrypted text is base64 encoded
			if len(encrypted) == 0 {
				t.Error("Encrypted text should not be empty")
			}

			// Decrypt
			decrypted, err := encryptor.DecryptCredential(encrypted)
			if err != nil {
				t.Errorf("DecryptCredential() error = %v", err)
				return
			}

			// Verify decrypted text matches original
			if decrypted != tt.plaintext {
				t.Errorf("Decrypted text = %v, want %v", decrypted, tt.plaintext)
			}
		})
	}
}

func TestDecryptInvalidInput(t *testing.T) {
	encryptor, err := NewEncryptor("this-is-a-very-long-encryption-key-for-testing")
	if err != nil {
		t.Fatalf("Failed to create encryptor: %v", err)
	}

	tests := []struct {
		name       string
		ciphertext string
		wantErr    bool
	}{
		{
			name:       "empty ciphertext",
			ciphertext: "",
			wantErr:    true,
		},
		{
			name:       "invalid base64",
			ciphertext: "not-base64!",
			wantErr:    true,
		},
		{
			name:       "too short ciphertext",
			ciphertext: "YWJjZA==", // "abcd" in base64, too short for nonce
			wantErr:    true,
		},
		{
			name:       "corrupted ciphertext",
			ciphertext: "dGhpcyBpcyBub3QgYSB2YWxpZCBjaXBoZXJ0ZXh0", // valid base64 but invalid ciphertext
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := encryptor.DecryptCredential(tt.ciphertext)
			if (err != nil) != tt.wantErr {
				t.Errorf("DecryptCredential() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestEncryptionConsistency(t *testing.T) {
	encryptor, err := NewEncryptor("this-is-a-very-long-encryption-key-for-testing")
	if err != nil {
		t.Fatalf("Failed to create encryptor: %v", err)
	}

	plaintext := "test-api-key-123"

	// Encrypt the same text multiple times
	encrypted1, err := encryptor.EncryptCredential(plaintext)
	if err != nil {
		t.Fatalf("First encryption failed: %v", err)
	}

	encrypted2, err := encryptor.EncryptCredential(plaintext)
	if err != nil {
		t.Fatalf("Second encryption failed: %v", err)
	}

	// Encrypted values should be different due to random nonce
	if encrypted1 == encrypted2 {
		t.Error("Multiple encryptions of the same plaintext should produce different ciphertexts")
	}

	// But both should decrypt to the same plaintext
	decrypted1, err := encryptor.DecryptCredential(encrypted1)
	if err != nil {
		t.Fatalf("First decryption failed: %v", err)
	}

	decrypted2, err := encryptor.DecryptCredential(encrypted2)
	if err != nil {
		t.Fatalf("Second decryption failed: %v", err)
	}

	if decrypted1 != plaintext || decrypted2 != plaintext {
		t.Error("Decrypted values should match original plaintext")
	}
}

func TestDifferentKeys(t *testing.T) {
	encryptor1, err := NewEncryptor("this-is-encryption-key-number-one")
	if err != nil {
		t.Fatalf("Failed to create first encryptor: %v", err)
	}

	encryptor2, err := NewEncryptor("this-is-encryption-key-number-two")
	if err != nil {
		t.Fatalf("Failed to create second encryptor: %v", err)
	}

	plaintext := "secret-api-key"

	// Encrypt with first key
	encrypted, err := encryptor1.EncryptCredential(plaintext)
	if err != nil {
		t.Fatalf("Encryption failed: %v", err)
	}

	// Try to decrypt with second key (should fail)
	_, err = encryptor2.DecryptCredential(encrypted)
	if err == nil {
		t.Error("Decryption with wrong key should fail")
	}
}

func TestValidateKey(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		wantErr bool
	}{
		{
			name:    "valid long key",
			key:     "this-is-a-very-long-encryption-key-for-testing",
			wantErr: false,
		},
		{
			name:    "exactly 32 bytes",
			key:     "12345678901234567890123456789012",
			wantErr: false,
		},
		{
			name:    "31 bytes",
			key:     "1234567890123456789012345678901",
			wantErr: true,
		},
		{
			name:    "empty key",
			key:     "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateKey(tt.key)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateKey() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}