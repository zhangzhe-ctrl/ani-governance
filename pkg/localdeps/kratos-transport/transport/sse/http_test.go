package sse

import (
	"bufio"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPStreamHandler(t *testing.T) {
	s := NewServer(
		WithAddress(":8800"),
	)
	defer s.Stop(nil)

	ctx := context.Background()

	mux := http.NewServeMux()
	mux.HandleFunc("/events", s.ServeHTTP)
	server := httptest.NewServer(mux)

	s.CreateStream("test")

	c := NewClient(server.URL + "/events")

	events := make(chan *Event)
	var cErr error
	go func() {
		cErr = c.Subscribe("test", func(msg *Event) {
			if msg.Data != nil {
				events <- msg
				return
			}
		})
	}()

	time.Sleep(time.Millisecond * 200)
	require.Nil(t, cErr)
	s.Publish(ctx, "test", &Event{Data: []byte("test")})

	msg, err := wait(events, time.Millisecond*500)
	require.Nil(t, err)
	assert.Equal(t, []byte(`test`), msg)
}

func TestHTTPStreamHandlerExistingEvents(t *testing.T) {
	s := NewServer(
		WithAddress(":8800"),
	)
	defer s.Stop(nil)

	ctx := context.Background()

	mux := http.NewServeMux()
	mux.HandleFunc("/events", s.ServeHTTP)
	server := httptest.NewServer(mux)

	s.CreateStream("test")

	s.Publish(ctx, "test", &Event{Data: []byte("test 1")})
	s.Publish(ctx, "test", &Event{Data: []byte("test 2")})
	s.Publish(ctx, "test", &Event{Data: []byte("test 3")})

	time.Sleep(time.Millisecond * 100)

	c := NewClient(server.URL + "/events")

	events := make(chan *Event)
	var cErr error
	go func() {
		cErr = c.Subscribe("test", func(msg *Event) {
			if len(msg.Data) > 0 {
				events <- msg
			}
		})
	}()

	require.Nil(t, cErr)

	for i := 1; i <= 3; i++ {
		msg, err := wait(events, time.Millisecond*500)
		require.Nil(t, err)
		assert.Equal(t, []byte("test "+strconv.Itoa(i)), msg)
	}
}

func TestHTTPStreamHandlerEventID(t *testing.T) {
	// The locked server logs every event when auto replay is on and EventLog.Add
	// replaces the id with a generated 32-hex UUIDv7, while Replay compares
	// "entry id >= cursor" as strings. The upstream case assumed ids "1"/"2"/"3",
	// which the implementation never produces. Keep the scenario — reconnect with a
	// cursor and see what the server replays — but drive it from the ids that were
	// actually delivered, and assert the whole replay window rather than "any event".
	s, base := startServing(t, WithPath("/events"))
	streamID := StreamID("test")
	s.CreateStream(streamID)

	collect := make(chan *Event, 8)
	c1 := NewClient(base + "/events")
	require.NoError(t, c1.SubscribeChan(string(streamID), collect))

	ctx := context.Background()
	published := []string{"test 1", "test 2", "test 3"}
	for _, data := range published {
		s.Publish(ctx, streamID, &Event{Data: []byte(data)})
	}

	type seen struct{ id, data string }
	var got []seen
	for len(got) < len(published) {
		select {
		case ev := <-collect:
			got = append(got, seen{id: string(ev.ID), data: string(ev.Data)})
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d of %d events arrived", len(got), len(published))
		}
	}
	c1.Unsubscribe(collect)

	for _, g := range got {
		require.Regexp(t, `^[0-9a-f]{32}$`, g.id, "delivered event ids are generated UUIDv7 hex")
	}
	// UUIDv7 is time ordered, so the delivered ids must be strictly increasing
	for i := 1; i < len(got); i++ {
		assert.Greater(t, got[i].id, got[i-1].id, "event ids are not in publication order")
	}

	cursor := got[1] // the second delivered event: ">=" means it is replayed too
	replay := make(chan *Event, 8)
	c2 := NewClient(base + "/events")
	c2.LastEventID.Store([]byte(cursor.id))
	require.NoError(t, c2.SubscribeChan(string(streamID), replay))

	var replayed []seen
	for _, data := range published[1:] {
		select {
		case ev := <-replay:
			replayed = append(replayed, seen{id: string(ev.ID), data: string(ev.Data)})
		case <-time.After(5 * time.Second):
			t.Fatalf("replay stopped early: got %v, wanted the tail from %q", replayed, data)
		}
	}
	select {
	case extra := <-replay:
		t.Errorf("unexpected third replayed event: id=%s data=%s", extra.ID, extra.Data)
	case <-time.After(500 * time.Millisecond):
	}
	c2.Unsubscribe(replay)

	require.Len(t, replayed, 2)
	assert.Equal(t, []string{"test 2", "test 3"}, []string{replayed[0].data, replayed[1].data},
		"the cursor event and everything after it must be replayed, in order")
	assert.Equal(t, cursor.id, replayed[0].id, "the replayed cursor event must keep its id")
	for _, r := range replayed {
		assert.NotEqual(t, got[0].data, r.data, "an event before the cursor must not be replayed")
	}
}

func TestHTTPStreamHandlerHeaderFlushIfNoEvents(t *testing.T) {
	// The upstream case bound the fixed port :8800, called the blocking Start on the
	// test goroutine, and then waited for SubscribeChan to *return*, which only
	// happens when the stream ends. The intent is that the response headers reach the
	// client while nothing has been published yet, so observe them through the
	// locked client's own ResponseValidator on a dynamic loopback address.
	s, base := startServing(t, WithPath("/events"))
	s.CreateStream("test")

	headers := make(chan http.Header, 1)
	events := make(chan *Event, 1)
	c := NewClient(base + "/events")
	c.ResponseValidator = func(_ *Client, resp *http.Response) error {
		headers <- resp.Header.Clone()
		return nil
	}

	require.NoError(t, c.SubscribeChan("test", events), "the stream never connected")

	var h http.Header
	select {
	case h = <-headers:
	case <-time.After(5 * time.Second):
		t.Fatal("the response headers never arrived")
	}
	assert.Equal(t, "text/event-stream", h.Get("Content-Type"))
	assert.Equal(t, "no-cache", h.Get("Cache-Control"))

	// nothing was published, so no event may be delivered
	select {
	case ev := <-events:
		t.Errorf("an event arrived although nothing was published: %v", ev)
	case <-time.After(500 * time.Millisecond):
	}
	c.Unsubscribe(events)
}

func TestHTTPStreamHandlerEventTTL(t *testing.T) {
	s := NewServer(
		WithAddress(":8800"),
	)
	defer s.Stop(nil)

	s.eventTTL = time.Second * 1

	ctx := context.Background()

	mux := http.NewServeMux()
	mux.HandleFunc("/events", s.ServeHTTP)
	server := httptest.NewServer(mux)

	s.CreateStream("test")

	s.Publish(ctx, "test", &Event{Data: []byte("test 1")})
	s.Publish(ctx, "test", &Event{Data: []byte("test 2")})
	time.Sleep(time.Second * 2)
	s.Publish(ctx, "test", &Event{Data: []byte("test 3")})

	time.Sleep(time.Millisecond * 100)

	c := NewClient(server.URL + "/events")

	events := make(chan *Event)
	var cErr error
	go func() {
		cErr = c.Subscribe("test", func(msg *Event) {
			if len(msg.Data) > 0 {
				events <- msg
			}
		})
	}()

	require.Nil(t, cErr)

	msg, err := wait(events, time.Millisecond*500)
	require.Nil(t, err)
	assert.Equal(t, []byte("test 3"), msg)
}

func TestHTTPStreamHandlerSubscriberCanReadAuthorizationToken(t *testing.T) {
	tokenCh := make(chan string, 1)

	s := NewServer(
		WithAddress(":8800"),
		WithSubscriberFunction(func(streamID StreamID, sub *Subscriber) {
			if streamID == "test" {
				tokenCh <- sub.Token("")
			}
		}),
	)
	defer s.Stop(nil)

	s.CreateStream("test")

	mux := http.NewServeMux()
	mux.HandleFunc("/events", s.ServeHTTP)
	server := httptest.NewServer(mux)
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/events?stream=test", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer header-token")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	select {
	case token := <-tokenCh:
		assert.Equal(t, "header-token", token)
	case <-time.After(time.Second):
		t.Fatal("subscriber callback did not receive token in time")
	}
}

func TestHTTPStreamHandlerAuthorizeUnauthorized(t *testing.T) {
	s := NewServer(
		WithAutoStream(true),
		WithAuthorizeFunc(func(_ *http.Request, token string) error {
			if token == "" {
				return ErrUnauthorized
			}
			return nil
		}),
	)
	defer s.Stop(nil)

	mux := http.NewServeMux()
	mux.HandleFunc("/events", s.ServeHTTP)
	server := httptest.NewServer(mux)
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/events?stream=test", nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestHTTPStreamHandlerAuthorizeForbidden(t *testing.T) {
	s := NewServer(
		WithAutoStream(true),
		WithAuthorizeFunc(func(_ *http.Request, token string) error {
			if token != "ok-token" {
				return errors.Join(ErrForbidden, errors.New("invalid token"))
			}
			return nil
		}),
	)
	defer s.Stop(nil)

	mux := http.NewServeMux()
	mux.HandleFunc("/events", s.ServeHTTP)
	server := httptest.NewServer(mux)
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/events?stream=test", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer bad-token")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestHTTPStreamHandlerAuthorizeSuccessWithAuthorizationHeader(t *testing.T) {
	s := NewServer(
		WithAutoStream(true),
		WithAuthorizeFunc(func(_ *http.Request, token string) error {
			if token != "ok-token" {
				return ErrUnauthorized
			}
			return nil
		}),
	)
	defer s.Stop(nil)

	mux := http.NewServeMux()
	mux.HandleFunc("/events", s.ServeHTTP)
	server := httptest.NewServer(mux)
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/events?stream=test", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer ok-token")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestHTTPStreamHandlerAutoStream(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	sseServer := NewServer()
	defer sseServer.Stop(nil)

	sseServer.autoReplay = false

	sseServer.autoStream = true

	mux := http.NewServeMux()
	mux.HandleFunc("/events", sseServer.ServeHTTP)
	srv := httptest.NewServer(mux)

	c := NewClient(srv.URL + "/events")

	events := make(chan *Event)

	cErr := make(chan error)

	go func() {
		cErr <- c.SubscribeChan("test", events)
	}()

	require.Nil(t, <-cErr)

	sseServer.Publish(ctx, "test", &Event{Data: []byte("test")})

	msg, err := wait(events, 1*time.Second)

	require.Nil(t, err)

	assert.Equal(t, []byte(`test`), msg)

	c.Unsubscribe(events)

	_, _ = wait(events, 1*time.Second)

	assert.Equal(t, (*Stream)(nil), sseServer.streamMgr.Get("test"))
}

// openUser subscribes over the real HTTP path with the bearer token that user owns
// and returns the delivered data payloads. A non-2xx answer closes the response and
// reports the status instead, so the caller can assert on the authorization outcome.
func openUser(t *testing.T, base, stream, token string) (<-chan string, int) {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, base+"/events?stream="+stream, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, resp.StatusCode
	}
	assert.True(t, strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream"),
		"an accepted subscription must be an event stream, got %q", resp.Header.Get("Content-Type"))

	frames := make(chan string, 128)
	go func() {
		defer close(frames)
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			if line := scanner.Text(); strings.HasPrefix(line, "data:") {
				frames <- strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			}
		}
	}()
	t.Cleanup(func() { _ = resp.Body.Close() })

	return frames, resp.StatusCode
}

// nextFrame returns the next delivered payload and fails the test at the deadline.
func nextFrame(t *testing.T, frames <-chan string) string {
	t.Helper()
	select {
	case data, ok := <-frames:
		if !ok {
			t.Fatal("the event stream ended before the payload arrived")
		}
		return data
	case <-time.After(5 * time.Second):
		t.Fatal("no event arrived within 5s")
		return ""
	}
}

// assertQuiet fails on any delivery inside the window. Silence is what proves the
// isolation, so the window has to be waited out rather than assumed.
func assertQuiet(t *testing.T, frames <-chan string, what string) {
	t.Helper()
	select {
	case data := <-frames:
		t.Fatalf("%s: received %q", what, data)
	case <-time.After(500 * time.Millisecond):
	}
}

// TestHTTPStreamHandlerTwoUserStreamIsolation drives the production authorization
// contract for two users on one served instance: the token decides the user id and
// ?stream= must name that same user, so a subscription can never attach to somebody
// else's stream and no delivery crosses streams.
func TestHTTPStreamHandlerTwoUserStreamIsolation(t *testing.T) {
	ctx := context.Background()

	// streamOwnerAuthorizer states HandleAuthorize's rule locally: "tok-<uid>"
	// authenticates uid and only ?stream=<uid> may then be subscribed.
	streamOwnerAuthorizer := func(r *http.Request, token string) error {
		uid := strings.TrimPrefix(token, "tok-")
		if uid == token || uid == "" {
			return ErrUnauthorized
		}
		if r.URL.Query().Get("stream") != uid {
			return ErrForbidden
		}
		return nil
	}

	s, base := startServing(t, WithPath("/events"), WithAuthorizeFunc(streamOwnerAuthorizer))
	s.CreateStream("1001")
	s.CreateStream("2002")

	frames, status := openUser(t, base, "1001", "tok-2002")
	require.Nil(t, frames, "a token must not subscribe another user's stream")
	assert.Equal(t, http.StatusForbidden, status)

	frames, status = openUser(t, base, "1001", "")
	require.Nil(t, frames, "an unauthenticated subscription must not be served")
	assert.Equal(t, http.StatusUnauthorized, status)

	alice, status := openUser(t, base, "1001", "tok-1001")
	require.NotNil(t, alice, "alice must be able to subscribe her own stream")
	require.Equal(t, http.StatusOK, status)

	bob, status := openUser(t, base, "2002", "tok-2002")
	require.NotNil(t, bob, "bob must be able to subscribe his own stream")
	require.Equal(t, http.StatusOK, status)

	s.Publish(ctx, "1001", &Event{Data: []byte("alice-1")})
	s.Publish(ctx, "1001", &Event{Data: []byte("alice-2")})
	assert.Equal(t, "alice-1", nextFrame(t, alice))
	assert.Equal(t, "alice-2", nextFrame(t, alice))
	assertQuiet(t, bob, "bob must not receive alice's stream")

	s.Publish(ctx, "2002", &Event{Data: []byte("bob-1")})
	assert.Equal(t, "bob-1", nextFrame(t, bob))
	assertQuiet(t, alice, "alice must not receive bob's stream")

	// both streams must still be alive and separate after the cross-talk checks
	assert.NotNil(t, s.streamMgr.Get("1001"))
	assert.NotNil(t, s.streamMgr.Get("2002"))
	assert.NotEqual(t, s.streamMgr.Get("1001"), s.streamMgr.Get("2002"))
}
