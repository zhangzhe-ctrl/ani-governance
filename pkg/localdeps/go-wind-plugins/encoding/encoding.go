package encoding

import (
	"strings"
	"sync"
)

// Codec defines the interface for encoding and decoding message bodies.
// It is the contract every concrete codec (json, proto, yaml …) must satisfy.
type Codec interface {
	Marshal(v any) ([]byte, error)
	Unmarshal(data []byte, v any) error
	Name() string
}

var (
	mu     sync.RWMutex
	codecs = map[string]Codec{}
)

// RegisterCodec registers a codec under its Name().
// The name is case-insensitive and stored in lower-case.
func RegisterCodec(c Codec) {
	if c == nil {
		panic("encoding: codec cannot be nil")
	}
	name := strings.ToLower(c.Name())
	if name == "" {
		panic("encoding: codec name cannot be empty")
	}
	mu.Lock()
	codecs[name] = c
	mu.Unlock()
}

// GetCodec returns the codec registered under name, or nil if not found.
// The name is case-insensitive.
func GetCodec(name string) Codec {
	if name == "" {
		return nil
	}
	name = strings.ToLower(name)
	mu.RLock()
	c := codecs[name]
	mu.RUnlock()
	return c
}
