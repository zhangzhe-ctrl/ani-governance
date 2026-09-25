package sse

import (
	"crypto/tls"
	"net"
	"net/http"
	"time"

	"github.com/go-kratos/kratos/v2/encoding"
)

// DefaultBufferSize is the default per-stream buffered event channel size.
const DefaultBufferSize = 1024

// ServerOption configures the SSE server.
type ServerOption func(o *Server)

// WithNetwork sets the listener network, such as tcp.
func WithNetwork(network string) ServerOption {
	return func(s *Server) {
		s.network = network
	}
}

// WithAddress sets the listener address.
func WithAddress(addr string) ServerOption {
	return func(s *Server) {
		s.address = addr
	}
}

// WithPath sets the HTTP route path handled by the SSE server.
func WithPath(path string) ServerOption {
	return func(s *Server) {
		s.path = path
	}
}

// WithTimeout sets the server timeout.
func WithTimeout(timeout time.Duration) ServerOption {
	return func(s *Server) {
		s.timeout = timeout
	}
}

// WithTLSConfig sets the TLS configuration used by the HTTP server.
func WithTLSConfig(c *tls.Config) ServerOption {
	return func(o *Server) {
		o.tlsConf = c
	}
}

// WithListener sets a custom listener.
func WithListener(lis net.Listener) ServerOption {
	return func(s *Server) {
		s.lis = lis
	}
}

// WithBufferSize sets the internal event channel buffer size per stream.
func WithBufferSize(size int) ServerOption {
	return func(s *Server) {
		s.bufferSize = size
	}
}

// WithCodec sets the codec used to marshal payloads in PublishData and NotifyData.
func WithCodec(c string) ServerOption {
	return func(s *Server) {
		s.codec = encoding.GetCodec(c)
	}
}

// WithEncodeBase64 enables or disables base64 encoding for event data.
func WithEncodeBase64(enable bool) ServerOption {
	return func(s *Server) {
		s.encodeBase64 = enable
	}
}

// WithAutoStream enables or disables automatic stream creation on subscribe.
func WithAutoStream(enable bool) ServerOption {
	return func(s *Server) {
		s.autoStream = enable
	}
}

// WithAutoReply enables or disables event replay for new subscribers.
func WithAutoReply(enable bool) ServerOption {
	return func(s *Server) {
		s.autoReplay = enable
	}
}

// WithSplitData enables or disables splitting event data by new lines.
func WithSplitData(enable bool) ServerOption {
	return func(s *Server) {
		s.splitData = enable
	}
}

// WithHeaders sets additional response headers for SSE responses.
func WithHeaders(headers map[string]string) ServerOption {
	return func(s *Server) {
		s.headers = headers
	}
}

// WithSubscriberFunction sets a callback invoked after a subscriber is added.
func WithSubscriberFunction(sub SubscriberFunction) ServerOption {
	return func(s *Server) {
		s.subscribeFunc = sub
	}
}

// WithUnSubscriberFunction sets a callback invoked after a subscriber is removed.
func WithUnSubscriberFunction(unsub SubscriberFunction) ServerOption {
	return func(s *Server) {
		s.unsubscribeFunc = unsub
	}
}

// WithEventTTL sets the event time-to-live used while streaming.
func WithEventTTL(timeout time.Duration) ServerOption {
	return func(s *Server) {
		s.eventTTL = timeout
	}
}

// WithStreamIdKey sets the query parameter key used to read the stream ID.
func WithStreamIdKey(key string) ServerOption {
	return func(s *Server) {
		s.streamIdKey = key
	}
}

// WithTokenExtractor sets the function used to extract auth tokens from requests.
func WithTokenExtractor(extractor TokenExtractor) ServerOption {
	return func(s *Server) {
		s.tokenExtractor = extractor
	}
}

// WithAuthorizeFunc sets the request authorization function.
func WithAuthorizeFunc(authorizeFn AuthorizeFunc) ServerOption {
	return func(s *Server) {
		s.authorizeFunc = authorizeFn
	}
}

// WithTokenHeader configures token extraction from a specific request header first.
func WithTokenHeader(headerKey string) ServerOption {
	return func(s *Server) {
		s.tokenExtractor = func(r *http.Request) string {
			if r == nil {
				return ""
			}

			if headerKey != "" {
				if token := r.Header.Get(headerKey); token != "" {
					return token
				}
			}

			return DefaultTokenExtractor(r)
		}
	}
}

// WithCORSAllowOrigin sets the Access-Control-Allow-Origin header value.
func WithCORSAllowOrigin(origin string) ServerOption {
	return func(s *Server) {
		s.corsAllowOrigin = origin
	}
}
