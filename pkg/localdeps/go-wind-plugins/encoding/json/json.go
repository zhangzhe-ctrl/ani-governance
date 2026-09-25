package json

import (
	"encoding/json"

	"go-wind-admin/pkg/localdeps/go-wind-plugins/encoding"
)

// Name is the name registered for the JSON codec.
const Name = "json"

func init() {
	encoding.RegisterCodec(codec{})
}

// codec implements encoding.Codec using encoding/json.
type codec struct{}

// Marshal encodes v into JSON bytes.
func (codec) Marshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

// Unmarshal decodes JSON data into v.
func (codec) Unmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

// Name returns the codec name.
func (codec) Name() string {
	return Name
}
