package crypto

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
)

// AccessKeyCipher is an explicit fail-closed cipher, independent of the optional
// global payload encryptor. Every service replica must load the same secret file.
type AccessKeyCipher struct{ encryptor *Encryptor }

func LoadAccessKeyCipherFromEnv() (*AccessKeyCipher, error) {
	path := os.Getenv("ANI_ACCESS_KEY_ENCRYPTION_KEY_FILE")
	if path == "" {
		return nil, errors.New("ANI_ACCESS_KEY_ENCRYPTION_KEY_FILE is required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read access key encryption key file: %w", err)
	}
	return NewAccessKeyCipher(strings.TrimSuffix(string(raw), "\n"))
}

func NewAccessKeyCipher(key string) (*AccessKeyCipher, error) {
	decoded, err := hex.DecodeString(key)
	if err != nil || len(key) != 64 || len(decoded) != 32 {
		return nil, errors.New("access key encryption key must be exactly 64 hexadecimal characters")
	}
	cipher, err := NewEncryptor(key)
	if err != nil {
		return nil, err
	}
	return &AccessKeyCipher{encryptor: cipher}, nil
}
func (c *AccessKeyCipher) Encrypt(secret string) (string, error) {
	if c == nil || c.encryptor == nil || secret == "" {
		return "", errors.New("access key encryption unavailable")
	}
	return c.encryptor.Encrypt(secret)
}
func (c *AccessKeyCipher) Decrypt(ciphertext string) (string, error) {
	if c == nil || c.encryptor == nil || !IsEncrypted(ciphertext) {
		return "", errors.New("access key ciphertext is missing or invalid")
	}
	plaintext, err := c.encryptor.Decrypt(ciphertext)
	if err != nil {
		return "", fmt.Errorf("decrypt access key secret: %w", err)
	}
	if !strings.HasPrefix(plaintext, "sk-") {
		return "", errors.New("invalid decrypted access key secret")
	}
	return plaintext, nil
}
