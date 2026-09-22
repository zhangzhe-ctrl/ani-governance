package crypto

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAccessKeyCipherFailClosed(t *testing.T) {
	for _, key := range []string{"", strings.Repeat("g", 64), strings.Repeat("a", 62), strings.Repeat("a", 64) + "\n"} {
		if _, err := NewAccessKeyCipher(key); err == nil {
			t.Fatal("accepted invalid key")
		}
	}
	c, err := NewAccessKeyCipher(strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := c.Encrypt("sk-unit-example")
	if err != nil {
		t.Fatal(err)
	}
	if encrypted == "sk-unit-example" || !IsEncrypted(encrypted) {
		t.Fatal("plaintext stored")
	}
	got, err := c.Decrypt(encrypted)
	if err != nil || got != "sk-unit-example" {
		t.Fatal("roundtrip failed")
	}
	for _, bad := range []string{"", "sk-plaintext", "enc:bad", encrypted[:len(encrypted)-5] + "aaaaa"} {
		if _, err = c.Decrypt(bad); err == nil {
			t.Fatal("accepted bad ciphertext")
		}
	}
	other, _ := NewAccessKeyCipher(strings.Repeat("b", 64))
	if _, err = other.Decrypt(encrypted); err == nil {
		t.Fatal("accepted wrong master key")
	}
	var missing *AccessKeyCipher
	if _, err = missing.Encrypt("sk-test"); err == nil {
		t.Fatal("missing cipher accepted")
	}
}
func TestAccessKeyCipherFile(t *testing.T) {
	t.Setenv("ANI_ACCESS_KEY_ENCRYPTION_KEY_FILE", "")
	if _, err := LoadAccessKeyCipherFromEnv(); err == nil {
		t.Fatal("missing key file accepted")
	}
	path := filepath.Join(t.TempDir(), "key")
	t.Setenv("ANI_ACCESS_KEY_ENCRYPTION_KEY_FILE", path)
	if err := os.WriteFile(path, []byte(strings.Repeat("a", 64)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAccessKeyCipherFromEnv(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("a", 64)+"\n\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAccessKeyCipherFromEnv(); err == nil {
		t.Fatal("multiple newlines accepted")
	}
}
