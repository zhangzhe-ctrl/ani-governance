package sse

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/signal"
	"syscall"
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

func TestServerExistingStreamPublish(t *testing.T) {
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	ctx := context.Background()

	s := NewServer(
		WithAddress(":8800"),
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
	defer s.Stop(ctx)

	s.CreateStream("test")

	stream := s.streamMgr.Get("test")
	sub := stream.addSubscriber("", nil)

	go func() {
		_ = s.Start(ctx)
	}()

	s.Publish(ctx, "test", &Event{Data: []byte("ping")})

	msg, err := wait(sub.connection, time.Second*1)
	require.Nil(t, err)
	assert.Equal(t, []byte(`ping`), msg)

	<-interrupt
}

func TestServerNonExistentStreamPublish(t *testing.T) {
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	ctx := context.Background()

	s := NewServer(
		WithAddress(":8800"),
		WithCodec("json"),
		WithPath("/events"),
	)
	defer s.Stop(ctx)

	s.CreateStream("test")

	go func() {
		s.streamMgr.RemoveWithID("test")
		_ = s.Start(ctx)
	}()

	assert.NotPanics(t, func() {
		_ = s.PublishData(ctx, "test", &Event{Data: []byte("test")})
	})

	<-interrupt
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
	s := NewServer(WithCodec("json"))
	defer s.Stop(ctx)

	s.CreateStream("test")
	stream := s.streamMgr.Get("test")
	require.NotNil(t, stream)

	sub := stream.addSubscriber("", nil)

	err := s.PublishDataWithMeta(ctx, "test", map[string]string{"message": "hello"},
		WithEventName("notification"),
		WithEventID("evt-001"),
		WithEventRetry("3000"),
	)
	require.NoError(t, err)

	ev, err := waitEvent(sub.connection, time.Second)
	require.NoError(t, err)
	require.NotNil(t, ev)
	assert.Equal(t, []byte("notification"), ev.Event)
	assert.Equal(t, []byte("evt-001"), ev.ID)
	assert.Equal(t, []byte("3000"), ev.Retry)

	var body map[string]string
	require.NoError(t, json.Unmarshal(ev.Data, &body))
	assert.Equal(t, "hello", body["message"])
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
