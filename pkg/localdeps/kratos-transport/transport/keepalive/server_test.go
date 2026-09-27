package keepalive

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
)

// TestKeepAliveService keeps the meaning of the upstream case — the server starts
// cleanly, stops cleanly and Start returns nil — while actually terminating.
// Server.Start is a blocking Serve in this transport, so the upstream form
// (assert.Nil(t, svc.Start(ctx)) on the test goroutine) could never return.
func TestKeepAliveService(t *testing.T) {
	// The default address is the host's primary interface plus a random port in
	// 10000-65535, which may already be taken. A loopback listener on port 0 is
	// both dynamic and local.
	svc := NewServer(WithNetwork("tcp"), WithAddress("127.0.0.1:0"))
	assert.NotNil(t, svc)

	bound, cancel := context.WithCancel(context.Background())
	defer cancel()

	returns := make(chan error, 1)
	go func() { returns <- svc.Start(bound) }()

	// awaitStartReturn consumes the single value Start produces exactly once, so the
	// body and the cleanup can both require convergence without blocking on a second
	// read of an already drained channel.
	var arrived bool
	awaitStartReturn := func(wait time.Duration) error {
		if arrived {
			return nil
		}
		select {
		case err := <-returns:
			arrived = true
			return err
		case <-time.After(wait):
			return fmt.Errorf("the Start goroutine did not return within %s", wait)
		}
	}

	var stopped bool
	stop := func() error {
		if stopped {
			return nil
		}
		stopped = true
		ctx, cancelStop := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancelStop()
		if err := svc.Stop(ctx); err != nil {
			return fmt.Errorf("Stop: %w", err)
		}
		return awaitStartReturn(20 * time.Second)
	}

	// cleanup is registered before anything can fail, and Stop always gets a live
	// context rather than nil
	t.Cleanup(func() {
		if err := stop(); err != nil {
			t.Errorf("shutdown: %v", err)
		}
	})

	// Readiness is proven with a real gRPC health request against the listener Start
	// itself bound. Endpoint() is deliberately not used to discover the address:
	// calling it before Start has bound makes Start reuse that listener and the
	// server then never answers, which this step reproduced.
	var endpoint string
	var lastErr error
	healthCheck := func(timeout time.Duration) (*grpc_health_v1.HealthCheckResponse, error) {
		lis := svc.lis
		if lis == nil {
			return nil, fmt.Errorf("Start has not bound a listener yet")
		}
		endpoint = lis.Addr().String()
		conn, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return nil, err
		}
		defer func() { _ = conn.Close() }()
		ctx, cancelCheck := context.WithTimeout(context.Background(), timeout)
		defer cancelCheck()
		return grpc_health_v1.NewHealthClient(conn).Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	}
	healthy := func() bool {
		res, err := healthCheck(2 * time.Second)
		if err != nil {
			lastErr = err
			return false
		}
		if res.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
			lastErr = fmt.Errorf("health status %v", res.GetStatus())
			return false
		}
		return true
	}
	if !assert.Eventually(t, healthy, 15*time.Second, 100*time.Millisecond) {
		t.Fatalf("the health service on %q never reported SERVING (last error: %v)", endpoint, lastErr)
	}

	assert.Equal(t, KindKeepAlive, svc.Name())

	// stop, and require the same nil the original case asserted on Start
	require.Nil(t, stop())
	assert.Nil(t, awaitStartReturn(20*time.Second), "Start must return nil on a clean shutdown")

	// the health service must be gone once the server stopped
	require.Eventually(t, func() bool {
		_, err := healthCheck(500 * time.Millisecond)
		return err != nil
	}, 10*time.Second, 200*time.Millisecond, "the health service still answered after Stop")
}
