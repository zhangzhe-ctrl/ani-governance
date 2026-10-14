package crypto

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
)

// AccessKeyCipher is an explicit fail-closed cipher, independent of the optional
// global payload encryptor. Every service replica must load the same secret file.
type AccessKeyCipher struct {
	encryptor *Encryptor
	// masterKey 是密钥文件里的 32 字节主密钥原文，供派生用途隔离的子密钥使用
	// （见 DeriveSubkey）。
	masterKey []byte
}

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
	// 主密钥原文另存一份：所有副本加载同一份密钥文件，
	// 因此由它派生的子密钥也天然一致。
	return &AccessKeyCipher{encryptor: cipher, masterKey: decoded}, nil
}

// DeriveSubkey 用主密钥派生用途隔离的子密钥（HMAC-SHA256(masterKey, label)）。
//
// 用于同一份主密钥下互不影响的独立用途（例如分页游标签名）。派生结果只依赖
// 主密钥与 label，因此在所有加载同一密钥文件的副本上完全一致，可用于跨副本
// 校验；轮换主密钥会同时使全部派生用途失效。
// 主密钥缺失或 label 为空时返回 nil，调用方应据此 fail-closed。
func (c *AccessKeyCipher) DeriveSubkey(label string) []byte {
	if c == nil || len(c.masterKey) == 0 || label == "" {
		return nil
	}
	mac := hmac.New(sha256.New, c.masterKey)
	mac.Write([]byte(label))
	return mac.Sum(nil)
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
