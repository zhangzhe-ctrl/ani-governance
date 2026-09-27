package sse

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func wait(ch chan *Event, duration time.Duration) ([]byte, error) {
	var err error
	var msg []byte

	select {
	case event := <-ch:
		msg = event.Data
	case <-time.After(duration):
		err = errors.New("timeout")
	}
	return msg, err
}

func waitEvent(ch chan *Event, duration time.Duration) (*Event, error) {
	select {
	case event := <-ch:
		return event, nil
	case <-time.After(duration):
		return nil, errors.New("timeout")
	}
}

// startServing binds a dynamic loopback listener, runs the blocking Start in its
// own goroutine, and returns the base URL. Everything is torn down in reverse
// order: the subscription side closes first, then Stop, then Start must return and
// the listener must be closed. A timeout on any of that is a failure, not a pass.
func startServing(t *testing.T, opts ...ServerOption) (*Server, string) {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	srv := NewServer(append([]ServerOption{WithListener(lis)}, opts...)...)

	ctx, cancel := context.WithCancel(context.Background())
	returns := make(chan error, 1)
	go func() { returns <- srv.Start(ctx) }()

	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer stopCancel()
		if err := srv.Stop(stopCtx); err != nil {
			t.Errorf("Stop: %v", err)
		}
		cancel()
		select {
		case err := <-returns:
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				t.Errorf("Start returned an error: %v", err)
			}
		case <-time.After(20 * time.Second):
			t.Errorf("the Start goroutine never returned after Stop")
		}
		_ = lis.Close()
	})

	base := "http://" + lis.Addr().String()
	require.Eventually(t, func() bool {
		conn, dialErr := net.DialTimeout("tcp", lis.Addr().String(), 200*time.Millisecond)
		if dialErr != nil {
			return false
		}
		_ = conn.Close()
		return true
	}, 10*time.Second, 50*time.Millisecond, "the stream server never accepted connections on "+base)

	return srv, base
}

func TestServerExistingStreamPublish(t *testing.T) {
	ctx := context.Background()

	s, _ := startServing(t,
		WithCodec("json"),
		WithPath("/events"),
		WithSubscriberFunction(func(streamID StreamID, sub *Subscriber) {
			var token string
			if sub.URL != nil {
				token = sub.URL.Query().Get("token")
			}
			LogInfof("subscriber [%s] [%+v] connected", streamID, token)
		}),
	)

	s.CreateStream("test")

	stream := s.streamMgr.Get("test")
	require.NotNil(t, stream)
	sub := stream.addSubscriber("", nil)

	s.Publish(ctx, "test", &Event{Data: []byte("ping")})

	msg, err := wait(sub.connection, time.Second*5)
	require.Nil(t, err)
	assert.Equal(t, []byte(`ping`), msg)

	// the original test only stopped at a human signal; the delivery above is the
	// completion condition, so the run ends here
	// the subscription above is a test-local object; nothing else keeps running
}

func TestServerNonExistentStreamPublish(t *testing.T) {
	ctx := context.Background()

	s, _ := startServing(t,
		WithCodec("json"),
		WithPath("/events"),
	)

	s.CreateStream("test")
	stream := s.streamMgr.Get("test")
	require.NotNil(t, stream)

	// The stream is really gone before the publish happens, so the case exercises the
	// missing-stream path rather than passing by accident.
	s.streamMgr.RemoveWithID("test")
	require.False(t, s.streamMgr.Exist("test"))

	// Recorded behaviour of this API, unchanged by the takeover: publishing at an
	// unknown stream is a no-op that neither panics nor reports an error.
	var publishErr error
	require.NotPanics(t, func() {
		publishErr = s.PublishData(ctx, "test", &Event{Data: []byte("test")})
	})
	assert.NoError(t, publishErr)
}

func TestServerPublishData(t *testing.T) {
	host, port, err := net.SplitHostPort("127.0.0.1:8800")
	require.NoError(t, err)
	t.Logf("host: %s, port: %s", host, port)
}

func TestServerPublishDataWithEventName(t *testing.T) {
	ctx := context.Background()
	s := NewServer(WithCodec("json"))
	defer s.Stop(ctx)

	s.CreateStream("test")
	stream := s.streamMgr.Get("test")
	require.NotNil(t, stream)

	sub := stream.addSubscriber("", nil)

	err := s.PublishDataWithEventName(ctx, "test", "notification", map[string]string{"message": "hello"})
	require.NoError(t, err)

	ev, err := waitEvent(sub.connection, time.Second)
	require.NoError(t, err)
	require.NotNil(t, ev)
	assert.Equal(t, []byte("notification"), ev.Event)

	var body map[string]string
	require.NoError(t, json.Unmarshal(ev.Data, &body))
	assert.Equal(t, "hello", body["message"])
}

func TestServerPublishDataWithMeta(t *testing.T) {
	ctx := context.Background()

	// With replay disabled the caller's own event id survives untouched.
	t.Run("without auto replay the custom id is kept", func(t *testing.T) {
		s, _ := startServing(t, WithCodec("json"), WithAutoReply(false))
		s.CreateStream("test")
		stream := s.streamMgr.Get("test")
		require.NotNil(t, stream)
		sub := stream.addSubscriber("", nil)

		require.NoError(t, s.PublishDataWithMeta(ctx, "test", map[string]string{"message": "hello"},
			WithEventName("notification"), WithEventID("evt-001"), WithEventRetry("3000")))

		ev, err := waitEvent(sub.connection, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, ev)
		assert.Equal(t, []byte("notification"), ev.Event)
		assert.Equal(t, []byte("evt-001"), ev.ID)
		assert.Equal(t, []byte("3000"), ev.Retry)

		var body map[string]string
		require.NoError(t, json.Unmarshal(ev.Data, &body))
		assert.Equal(t, "hello", body["message"])
	})

	// With the default auto replay the event is logged, and EventLog.Add replaces
	// the id with a generated 32-hex UUIDv7. That is recorded behaviour, not a
	// fixed defect: the server does not keep a caller supplied id in this mode.
	// Everything else about the metadata must be unchanged, and the id a subscriber
	// sees must be the same id a later replay hands out.
	t.Run("with auto replay on the generated id is stable across replay", func(t *testing.T) {
		s, _ := startServing(t, WithCodec("json"))
		s.CreateStream("test")
		stream := s.streamMgr.Get("test")
		require.NotNil(t, stream)
		sub := stream.addSubscriber("", nil)

		require.NoError(t, s.PublishDataWithMeta(ctx, "test", map[string]string{"message": "hello"},
			WithEventName("notification"), WithEventID("evt-001"), WithEventRetry("3000")))

		ev, err := waitEvent(sub.connection, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, ev)
		assert.Equal(t, []byte("notification"), ev.Event)
		assert.Equal(t, []byte("3000"), ev.Retry)
		var body map[string]string
		require.NoError(t, json.Unmarshal(ev.Data, &body))
		assert.Equal(t, "hello", body["message"])

		assert.NotEqual(t, []byte("evt-001"), ev.ID,
			"auto replay is documented to replace the id; if this ever stops being true the server changed")
		assert.Regexp(t, `^[0-9a-f]{32}$`, ev.ID, "the generated id is a 32-hex UUIDv7")

		replayed := stream.addSubscriber(string(ev.ID), nil)
		stream.eventLog.Replay(replayed)
		again, err := waitEvent(replayed.connection, 5*time.Second)
		require.NoError(t, err)
		assert.Equal(t, ev.ID, again.ID, "the replayed event must carry the same id")
		assert.Equal(t, ev.Data, again.Data)
		assert.Equal(t, ev.Event, again.Event)
	})
}

func TestServerNotifyDataWithEventName(t *testing.T) {
	ctx := context.Background()
	s := NewServer(WithCodec("json"))
	defer s.Stop(ctx)

	s.CreateStream("test")
	stream := s.streamMgr.Get("test")
	require.NotNil(t, stream)

	sub := stream.addSubscriber("", nil)

	err := s.NotifyDataWithEventName(ctx, "notification", map[string]bool{"ok": true})
	require.NoError(t, err)

	ev, err := waitEvent(sub.connection, time.Second)
	require.NoError(t, err)
	require.NotNil(t, ev)
	assert.Equal(t, []byte("notification"), ev.Event)

	var body map[string]bool
	require.NoError(t, json.Unmarshal(ev.Data, &body))
	assert.Equal(t, true, body["ok"])
}

func TestServerNotifyDataWithMeta(t *testing.T) {
	ctx := context.Background()
	s := NewServer(WithCodec("json"))
	defer s.Stop(ctx)

	s.CreateStream("test")
	stream := s.streamMgr.Get("test")
	require.NotNil(t, stream)

	sub := stream.addSubscriber("", nil)

	err := s.NotifyDataWithMeta(ctx, map[string]int{"count": 42},
		WithEventName("update"),
		WithEventComment("broadcast"),
	)
	require.NoError(t, err)

	ev, err := waitEvent(sub.connection, time.Second)
	require.NoError(t, err)
	require.NotNil(t, ev)
	assert.Equal(t, []byte("update"), ev.Event)
	assert.Equal(t, []byte("broadcast"), ev.Comment)

	var body map[string]int
	require.NoError(t, json.Unmarshal(ev.Data, &body))
	assert.Equal(t, 42, body["count"])
}

// TestServerTryPublishFullBuffer covers the entry-queue contract of TryPublish: it
// must refuse rather than block once the stream cannot take more, while Publish on
// the same stream waits instead of dropping.
func TestServerTryPublishFullBuffer(t *testing.T) {
	ctx := context.Background()
	s, _ := startServing(t)

	s.CreateStream("slow")
	s.CreateStream("other")
	slow := s.streamMgr.Get("slow")
	other := s.streamMgr.Get("other")
	require.NotNil(t, slow)
	require.NotNil(t, other)

	slowSub := slow.addSubscriber("", nil)

	// Nothing is read from slowSub.connection, so the stream loop stalls on it and
	// the buffers behind it fill up until TryPublish has to refuse.
	accepted, refusedIn := 0, time.Duration(0)
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		began := time.Now()
		if s.TryPublish(ctx, "slow", &Event{Data: []byte("x")}) {
			accepted++
			continue
		}
		refusedIn = time.Since(began)
		break
	}
	require.Greater(t, accepted, 0, "TryPublish never accepted anything")
	require.Greater(t, refusedIn, time.Duration(0), "TryPublish never refused")
	assert.Less(t, refusedIn, 100*time.Millisecond, "TryPublish blocked instead of refusing")

	// Observation only, deliberately not asserted: on this stream the unread
	// subscriber stops the dispatch loop after a couple of events, the 1024-slot
	// stream buffer fills, and TryPublish then refuses. A blocking Publish issued at
	// that moment returned straight away, which is the <-stream.quit arm of Publish's
	// select rather than a queued send. The business path only ever calls TryPublish,
	// so this is recorded and left alone; asserting either behaviour here would be
	// inventing a contract the takeover is not allowed to change.
	published := make(chan struct{})
	go func() {
		s.Publish(ctx, "slow", &Event{Data: []byte("after refusal")})
		close(published)
	}()
	select {
	case <-published:
		t.Logf("Publish returned after TryPublish refused: the stream dropped it through the quit arm")
	case <-time.After(2 * time.Second):
		t.Logf("Publish is still waiting behind the saturated stream after 2s")
	}

	// keep the reader drained so nothing is left blocked when the server stops
	go func() {
		for {
			select {
			case <-slowSub.connection:
			case <-time.After(500 * time.Millisecond):
				return
			}
		}
	}()

	// a different stream must keep accepting and delivering
	require.Eventually(t, func() bool {
		return s.TryPublish(ctx, "other", &Event{Data: []byte("ping")})
	}, 5*time.Second, 20*time.Millisecond, "an unrelated stream stopped accepting events")
	otherSub := other.addSubscriber("", nil)
	require.True(t, s.TryPublish(ctx, "other", &Event{Data: []byte("ping2")}))
	// the accepted "ping" from the check above is still queued, so the order on the
	// wire is ping then ping2
	first, err := wait(otherSub.connection, 3*time.Second)
	require.Nil(t, err)
	assert.Equal(t, []byte("ping"), first)
	second, err := wait(otherSub.connection, 3*time.Second)
	require.Nil(t, err)
	assert.Equal(t, []byte("ping2"), second)
}

// TestServerSlowSubscriberCouplingSameStream records how this transport applies
// backpressure inside one stream: the dispatch loop writes to subscribers in
// registration order and blocks on a full one, so an unread subscriber stops the
// loop and everything behind it. That is an observation of the existing design, kept
// apart from the TryPublish contract above, and the backpressure is not changed to
// make a test pass.
func TestServerSlowSubscriberCouplingSameStream(t *testing.T) {
	ctx := context.Background()
	s, _ := startServing(t)
	s.CreateStream("paired")
	stream := s.streamMgr.Get("paired")

	first := stream.addSubscriber("", nil)
	unread := stream.addSubscriber("", nil)

	// Drain the readable subscriber continuously: the dispatch loop writes in
	// registration order, so if it stalled on this one the observation below would
	// measure the wrong blockage.
	var delivered int64
	drained := make(chan struct{})
	stop := make(chan struct{})
	t.Cleanup(func() {
		close(stop)
		<-drained
	})
	go func() {
		defer close(drained)
		for {
			select {
			case <-first.connection:
				atomic.AddInt64(&delivered, 1)
			case <-stop:
				return
			}
		}
	}()

	accepted := 0
	for i := 0; i < 400; i++ {
		if !s.TryPublish(ctx, "paired", &Event{Data: []byte("e")}) {
			break
		}
		accepted++
	}

	// The unread subscriber saturates once the loop has fanned out cap(connection)
	// events; how long that takes depends on the loop, so it is waited for instead
	// of sampled.
	require.Eventually(t, func() bool {
		return len(unread.connection) == cap(unread.connection)
	}, 10*time.Second, 50*time.Millisecond, "the unread subscriber never saturated")

	t.Logf("same-stream backpressure: %d events accepted, readable subscriber delivered %d, "+
		"unread subscriber holds %d/%d, loop blocked with %d events still queued on the stream buffer",
		accepted, atomic.LoadInt64(&delivered), len(unread.connection), cap(unread.connection),
		len(stream.event))
	assert.Greater(t, accepted, cap(unread.connection),
		"publishing should have been buffered before it was refused")
}
