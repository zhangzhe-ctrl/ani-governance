package sse

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gopkg.in/cenkalti/backoff.v1"
)

var urlPath string
var srv *Server
var server *httptest.Server

var mldata = `{
	"key": "value",
	"array": [
		1,
		2,
		3
	]
}`

func setup(empty bool) {
	srv = newServer()
	go publishMsgs(srv, empty, 100000000)
}

func setupMultiline() {
	srv = newServer()
	srv.splitData = true
	go publishMultilineMessages(srv, 100000000)
}

func setupCount(empty bool, count int) {
	srv = newServer()
	go publishMsgs(srv, empty, count)
}

func newServer() *Server {
	srv = NewServer()

	mux := http.NewServeMux()
	mux.HandleFunc("/events", srv.ServeHTTP)
	server = httptest.NewServer(mux)
	urlPath = server.URL + "/events"

	srv.CreateStream("test")

	return srv
}

func newServer401() *Server {
	srv = NewServer()

	mux := http.NewServeMux()
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})

	server = httptest.NewServer(mux)
	urlPath = server.URL + "/events"

	srv.CreateStream("test")

	return srv
}

func publishMsgs(s *Server, empty bool, count int) {
	ctx := context.Background()

	for a := 0; a < count; a++ {
		if empty {
			s.Publish(ctx, "test", &Event{Data: []byte("\n")})
		} else {
			s.Publish(ctx, "test", &Event{Data: []byte("ping")})
		}
		time.Sleep(time.Millisecond * 50)
	}
}

func publishMultilineMessages(s *Server, count int) {
	ctx := context.Background()
	for a := 0; a < count; a++ {
		s.Publish(ctx, "test", &Event{ID: []byte("123456"), Data: []byte(mldata)})
	}
}

func cleanup() {
	server.CloseClientConnections()
	server.Close()
	_ = srv.Stop(nil)
}

func TestClientSubscribe(t *testing.T) {
	setup(false)
	defer cleanup()

	c := NewClient(urlPath)

	events := make(chan *Event, 1)
	var cErr error
	go func() {
		cErr = c.Subscribe("test", func(msg *Event) {
			if msg.Data != nil {
				fmt.Println("recv message: ", string(msg.Data))
				events <- msg
				return
			}
		})
	}()

	for i := 0; i < 5; i++ {
		msg, err := wait(events, time.Second*1)
		require.Nil(t, err)
		assert.Equal(t, []byte(`ping`), msg)
	}

	assert.Nil(t, cErr)
}

func TestClientSubscribeMultiline(t *testing.T) {

	setupMultiline()
	defer cleanup()

	c := NewClient(urlPath)

	events := make(chan *Event)
	var cErr error

	go func() {
		cErr = c.Subscribe("test", func(msg *Event) {
			if msg.Data != nil {
				fmt.Println("recv message: ", string(msg.Data))
				events <- msg
				return
			}
		})
	}()

	for i := 0; i < 5; i++ {
		msg, err := wait(events, time.Second*1)
		require.Nil(t, err)
		assert.Equal(t, []byte(mldata), msg)
	}

	assert.Nil(t, cErr)
}

func TestClientChanSubscribeEmptyMessage(t *testing.T) {
	setup(true)
	defer cleanup()

	c := NewClient(urlPath)

	events := make(chan *Event)
	err := c.SubscribeChan("test", events)
	require.Nil(t, err)

	for i := 0; i < 5; i++ {
		_, err := waitEvent(events, time.Second)
		require.Nil(t, err)
	}
}

func TestClientChanSubscribe(t *testing.T) {
	setup(false)
	defer cleanup()

	c := NewClient(urlPath)

	events := make(chan *Event)
	err := c.SubscribeChan("test", events)
	require.Nil(t, err)

	for i := 0; i < 5; i++ {
		msg, merr := wait(events, time.Second*1)
		if msg == nil {
			i--
			continue
		}
		assert.Nil(t, merr)
		assert.Equal(t, []byte(`ping`), msg)
	}
	c.Unsubscribe(events)
}

func TestClientOnDisconnect(t *testing.T) {
	setup(false)
	defer cleanup()

	c := NewClient(urlPath)

	called := make(chan struct{})
	c.OnDisconnect(func(client *Client) {
		called <- struct{}{}
	})

	go c.Subscribe("test", func(msg *Event) {})

	time.Sleep(time.Second)
	server.CloseClientConnections()

	assert.Equal(t, struct{}{}, <-called)
}

func TestClientOnConnect(t *testing.T) {
	setup(false)
	defer cleanup()

	c := NewClient(urlPath)

	called := make(chan struct{})
	c.OnConnect(func(client *Client) {
		called <- struct{}{}
	})

	go c.Subscribe("test", func(msg *Event) {})

	time.Sleep(time.Second)
	assert.Equal(t, struct{}{}, <-called)

	server.CloseClientConnections()
}

func TestClientChanReconnect(t *testing.T) {
	setup(false)
	defer cleanup()

	c := NewClient(urlPath)

	events := make(chan *Event)
	err := c.SubscribeChan("test", events)
	require.Nil(t, err)

	for i := 0; i < 10; i++ {
		if i == 5 {
			server.CloseClientConnections()
		}
		msg, merr := wait(events, time.Second*1)
		if msg == nil {
			i--
			continue
		}
		assert.Nil(t, merr)
		assert.Equal(t, []byte(`ping`), msg)
	}
	c.Unsubscribe(events)
}

func TestClientUnsubscribe(t *testing.T) {
	setup(false)
	defer cleanup()

	c := NewClient(urlPath)

	events := make(chan *Event)
	err := c.SubscribeChan("test", events)
	require.Nil(t, err)

	time.Sleep(time.Millisecond * 500)

	go c.Unsubscribe(events)
	go c.Unsubscribe(events)
}

func TestClientUnsubscribeNonBlock(t *testing.T) {
	count := 2
	setupCount(false, count)
	defer cleanup()

	c := NewClient(urlPath)

	events := make(chan *Event)
	err := c.SubscribeChan("test", events)
	require.Nil(t, err)

	for i := 0; i < count; i++ {
		msg, merr := wait(events, time.Second*1)
		assert.Nil(t, merr)
		assert.Equal(t, []byte(`ping`), msg)
	}
	doneCh := make(chan *Event)
	go func() {
		var e Event
		c.Unsubscribe(events)
		doneCh <- &e
	}()
	_, merr := wait(doneCh, time.Millisecond*100)
	assert.Nil(t, merr)
}

func TestClientUnsubscribe401(t *testing.T) {
	srv = newServer401()
	defer cleanup()

	c := NewClient(urlPath)

	c.ReconnectStrategy = backoff.WithMaxTries(
		backoff.NewExponentialBackOff(),
		3,
	)

	err := c.SubscribeRaw(func(ev *Event) {
		assert.False(t, true)
	})

	require.NotNil(t, err)
}

func TestClientLargeData(t *testing.T) {
	srv = newServer()
	defer cleanup()

	ctx := context.Background()

	c := NewClient(urlPath, ClientMaxBufferSize(1<<19))

	c.ReconnectStrategy = backoff.WithMaxTries(
		backoff.NewExponentialBackOff(),
		3,
	)

	data := make([]byte, 1<<17)
	rand.Read(data)
	data = []byte(hex.EncodeToString(data))

	ec := make(chan *Event, 1)

	srv.Publish(ctx, "test", &Event{Data: data})

	go func() {
		_ = c.Subscribe("test", func(ev *Event) {
			ec <- ev
		})
	}()

	d, err := wait(ec, time.Second)
	require.Nil(t, err)
	require.Equal(t, data, d)
}

func TestClientComment(t *testing.T) {
	srv = newServer()
	defer cleanup()

	ctx := context.Background()

	c := NewClient(urlPath)

	events := make(chan *Event)
	err := c.SubscribeChan("test", events)
	require.Nil(t, err)

	srv.Publish(ctx, "test", &Event{Comment: []byte("comment")})
	srv.Publish(ctx, "test", &Event{Data: []byte("test")})

	ev, err := waitEvent(events, time.Second*1)
	assert.Nil(t, err)
	assert.Equal(t, []byte("test"), ev.Data)

	c.Unsubscribe(events)
}

func TestTrimHeader(t *testing.T) {
	tests := []struct {
		input []byte
		want  []byte
	}{
		{
			input: []byte("data: real data"),
			want:  []byte("real data"),
		},
		{
			input: []byte("data:real data"),
			want:  []byte("real data"),
		},
		{
			input: []byte("data:"),
			want:  []byte(""),
		},
	}

	for _, tc := range tests {
		got := trimHeader(len(headerData), tc.input)
		require.Equal(t, tc.want, got)
	}
}

// childEnv marks the re-executed copy of this test binary that runs the scenario alone, so a
// process-wide stack sample measures this test rather than every SSE case that ran before it.
const childEnv = "ANI_SSE_CANCEL_CHILD"

// probeEnv tells that child to keep one subscription open, which must make the acceptance fail.
const probeEnv = "ANI_SSE_LEAK_PROBE"

// ownedSubscriptions is the ten concurrent subscriptions the original case started, and exitWindow is
// the bound it always allowed for them to finish after the cancellation.
const (
	ownedSubscriptions = 10
	exitWindow         = 20 * time.Second
	readyWindow        = 15 * time.Second
)

// TestSubscribeWithContextDone keeps its name, its ten concurrent subscriptions, the real client over a
// real HTTP event stream, the same context cancellation and the same 20 second exit bound. What changed
// is what is measured. The old case compared one process-wide count against a baseline sampled from the
// same global count, and that count included the Client goroutines every earlier case in the binary had
// left behind: it moved during the run, so the equality was not a property of this scenario at all. Now
// each of the ten subscriptions reports its own readiness from inside its own handler and its own return
// by number, and the strict "no client goroutine is left" statement is made only in a child process that
// runs nothing else - where the whole-process sample is exactly this test.
func TestSubscribeWithContextDone(t *testing.T) {
	if os.Getenv(childEnv) != "" {
		report := runCancelScenario(t, os.Getenv(probeEnv) == "1")
		report.log(t)
		if fail := report.acceptance(); fail != "" {
			t.Errorf("child scenario failed: %s", fail)
		}
		return
	}

	// In this process other cases may still hold Client goroutines, so the inline run judges only what
	// it owns: ten readiness signals, ten distinct returns, and its own server-side streams closed.
	online := runCancelScenario(t, false)
	online.log(t)
	for _, fail := range []string{online.acceptanceOwnSignals(), online.acceptanceServerSideStreams()} {
		if fail != "" {
			t.Fatalf("inline scenario failed: %s", fail)
		}
	}

	// The same scenario in a process that ran nothing else, where a residual client goroutine can only
	// be one this scenario started. The child is this very test binary, so it carries the same
	// production code and, when the parent was built with -race, the same instrumentation.
	if out, err := runChild(t, false); err != nil {
		t.Fatalf("the isolated child rejected a correct scenario: %v\n%s", err, out)
	} else {
		t.Logf("isolated child passed:\n%s", out)
	}
}

// TestSubscribeWithContextDoneControls keeps the rewritten acceptance honest rather than merely
// satisfiable: a withheld or duplicated completion must fail the wait, the stack collector must prove it
// notices an incomplete dump instead of parsing one, and a subscription that is deliberately left open
// must make the isolated acceptance fail.
func TestSubscribeWithContextDoneControls(t *testing.T) {
	t.Run("a withheld completion fails the wait", func(t *testing.T) {
		done := make(chan subOutcome, 10)
		for i := 0; i < 9; i++ {
			done <- subOutcome{index: i, at: time.Now()}
		}
		close(done)
		started := time.Now()
		outcomes, err := collectCompletions(done, ownedSubscriptions, 500*time.Millisecond)
		require.Error(t, err, "nine of ten completions must not satisfy a ten of ten wait")
		require.Len(t, outcomes, 9)
		require.Less(t, time.Since(started), 5*time.Second, "the wait must return when its deadline passes")
	})

	t.Run("a duplicated completion fails the wait", func(t *testing.T) {
		done := make(chan subOutcome, 10)
		for i := 0; i < 9; i++ {
			done <- subOutcome{index: i, at: time.Now()}
		}
		done <- subOutcome{index: 0, at: time.Now()}
		close(done)
		_, err := collectCompletions(done, ownedSubscriptions, 500*time.Millisecond)
		require.Error(t, err, "one subscription reporting twice must not pass for ten distinct exits")
	})

	t.Run("the collector notices an incomplete dump and grows past it", func(t *testing.T) {
		// One raw 64 byte sample cannot cover this process, and must say so instead of handing back a
		// partial set that looks like a count.
		raw := stackSnapshot(64)
		require.False(t, raw.complete,
			"64 bytes held %d of %d goroutine stacks and was reported as complete", len(raw.blocks), raw.goroutines)
		require.Less(t, len(raw.blocks), raw.goroutines)

		grown := collectClientStacks(64)
		require.True(t, grown.complete, "the growth loop has to reach a dump that covers every goroutine")
		require.Greater(t, grown.bufferSize, 64, "starting tiny and never growing would be the same blind spot")
		require.GreaterOrEqual(t, len(grown.blocks), grown.goroutines)
		require.Less(t, grown.used, grown.bufferSize)
	})

	t.Run("an open subscription fails the isolated acceptance", func(t *testing.T) {
		out, err := runChild(t, true)
		require.Error(t, err, "a subscription that never returned must not pass the exit check:\n%s", out)
		require.Contains(t, string(out), "outlived the cancel", "the failure has to name what stayed open")
	})
}

// subOutcome is one subscription's return, identified by the index that started it.
type subOutcome struct {
	index int
	err   error
	at    time.Time
}

// scenarioReport carries everything the acceptance judges, plus the raw numbers behind each claim.
type scenarioReport struct {
	begun       time.Time
	readyAt     time.Time
	cancelledAt time.Time
	ready       int
	returned    []subOutcome
	waitErr     error
	requests    int64
	streamsOpen int64
	probeErr    error
	start       stackSample
	end         stackSample
}

// acceptanceOwnSignals is the part that needs no process-wide view: all ten really started receiving,
// and all ten really came back.
func (r scenarioReport) acceptanceOwnSignals() string {
	if r.ready != ownedSubscriptions {
		return fmt.Sprintf("only %d of %d subscriptions entered event receiving within %s", r.ready, ownedSubscriptions, readyWindow)
	}
	if r.waitErr != nil {
		return fmt.Sprintf("%v", r.waitErr)
	}
	return ""
}

// acceptanceServerSideStreams checks the resources this test's own fixture handed out: every HTTP
// stream it opened has to be gone, which is how a readLoop that never noticed the cancellation shows up.
func (r scenarioReport) acceptanceServerSideStreams() string {
	if r.streamsOpen != 0 {
		return fmt.Sprintf("%d server-side streams outlived the cancellation (%d requests seen)", r.streamsOpen, r.requests)
	}
	return ""
}

// acceptance is the full judgement, including the process-wide residue that only the child may claim.
func (r scenarioReport) acceptance() string {
	if fail := r.acceptanceOwnSignals(); fail != "" {
		return fail
	}
	if fail := r.acceptanceServerSideStreams(); fail != "" {
		return fail
	}
	if !r.end.complete {
		return fmt.Sprintf("the stack sample was never complete (buffer %d bytes, %d of %d goroutines)",
			r.end.bufferSize, len(r.end.blocks), r.end.goroutines)
	}
	if r.end.matching != 0 {
		return fmt.Sprintf("%d client goroutines outlived the cancel: %s", r.end.matching, r.end.topFrames())
	}
	return ""
}

func (r scenarioReport) log(t *testing.T) {
	t.Logf("began %s; all %d ready +%s; cancelled +%s; completions=%d waitErr=%v",
		r.begun.Format(time.RFC3339Nano), ownedSubscriptions, r.readyAt.Sub(r.begun), r.cancelledAt.Sub(r.begun),
		len(r.returned), r.waitErr)
	for _, o := range r.returned {
		t.Logf("  subscription %2d returned %s after cancel, err=%v", o.index, o.at.Sub(r.cancelledAt).Round(time.Microsecond), o.err)
	}
	if r.probeErr != nil {
		t.Logf("  probe subscription: %v", r.probeErr)
	}
	t.Logf("fixture saw %d HTTP requests; %d of its streams were still open at the deadline", r.requests, r.streamsOpen)
	t.Logf("stack sample at start: buffer=%d used=%d goroutines=%d blocks=%d complete=%t matching=%d",
		r.start.bufferSize, r.start.used, r.start.goroutines, len(r.start.blocks), r.start.complete, r.start.matching)
	t.Logf("stack sample at end:   buffer=%d used=%d goroutines=%d blocks=%d complete=%t matching=%d",
		r.end.bufferSize, r.end.used, r.end.goroutines, len(r.end.blocks), r.end.complete, r.end.matching)
}

// runCancelScenario drives the real thing: ten subscriptions on one live SSE fixture, readiness proven
// from inside each handler, cancellation only after all ten are receiving, then the bounded wait for ten
// distinct returns. With holdOne it also keeps an eleventh subscription on a context that is never
// cancelled - the leak the controls must catch - and releases it only after the verdict, so the fixture
// cannot deadlock waiting for a stream this test deliberately left open.
func runCancelScenario(t *testing.T, holdOne bool) scenarioReport {
	t.Helper()

	var (
		report   scenarioReport
		requests atomic.Int64
		open     atomic.Int64
	)
	publish, stopPublisher := context.WithCancel(context.Background())
	defer stopPublisher()

	mux := http.NewServeMux()
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Errorf("the test transport cannot flush")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		open.Add(1)
		defer open.Add(-1)

		// The handler must never block on the client, and it stops with the request or the fixture, so
		// no publisher of this test outlives the case.
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-publish.Done():
				return
			case <-tick.C:
				if _, err := fmt.Fprint(w, "data: ping\n\n"); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	})
	fixture := httptest.NewServer(mux)
	url := fixture.URL + "/events"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		ready  = make(chan int, ownedSubscriptions+1)
		done   = make(chan subOutcome, ownedSubscriptions+1)
		probe  = make(chan subOutcome, 1)
		extra  context.Context
		stopIt context.CancelFunc
	)
	start := func(index int, runCtx context.Context, sink chan subOutcome) {
		c := NewClient(url)
		var once sync.Once
		go func() {
			err := c.SubscribeWithContext(runCtx, "", func(msg *Event) {
				once.Do(func() { ready <- index })
			})
			sink <- subOutcome{index: index, err: err, at: time.Now()}
		}()
	}

	report.begun = time.Now()
	for i := 0; i < ownedSubscriptions; i++ {
		start(i, ctx, done)
	}
	if holdOne {
		extra, stopIt = context.WithCancel(context.Background())
		defer stopIt()
		start(ownedSubscriptions, extra, probe)
	}

	report.ready = waitReady(ready, ownedSubscriptions, readyWindow)
	if report.ready == ownedSubscriptions {
		report.readyAt = time.Now()
		report.start = collectClientStacks(1 << 10)
		report.cancelledAt = time.Now()
		cancel()
		report.returned, report.waitErr = collectCompletions(done, ownedSubscriptions, exitWindow)
	}

	// The judgement happens before anything is force-closed: a server that is shut down first could hide
	// an implementation that only exits because its stream was cut.
	settleDeadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(settleDeadline) && open.Load() > 0 {
		time.Sleep(20 * time.Millisecond)
	}
	report.requests = requests.Load()
	report.streamsOpen = open.Load()
	report.end = collectClientStacks(1 << 10)

	if holdOne {
		// Only now release the probe, and record what it reported: the acceptance above already saw it.
		stopIt()
		select {
		case o := <-probe:
			report.probeErr = o.err
		case <-time.After(2 * time.Second):
			report.probeErr = fmt.Errorf("the probe subscription never returned")
		}
	}

	fixture.CloseClientConnections()
	fixture.Close()
	return report
}

// waitReady counts distinct readiness signals, returning how many arrived before the deadline.
func waitReady(ready chan int, want int, timeout time.Duration) int {
	seen := map[int]bool{}
	deadline := time.After(timeout)
	for len(seen) < want {
		select {
		case index := <-ready:
			seen[index] = true
		case <-deadline:
			return len(seen)
		}
	}
	return len(seen)
}

// collectCompletions waits for want distinct subscriptions to return and says which ones did not.
func collectCompletions(done <-chan subOutcome, want int, timeout time.Duration) ([]subOutcome, error) {
	var (
		outcomes []subOutcome
		seen     = map[int]bool{}
		deadline = time.After(timeout)
	)
	for len(outcomes) < want {
		select {
		case o, ok := <-done:
			if !ok {
				return outcomes, fmt.Errorf("the completion channel closed with %d of %d subscriptions back", len(outcomes), want)
			}
			if o.index < 0 || o.index >= want {
				continue // a leak probe's exit is judged by the residue check, not as one of the ten
			}
			if seen[o.index] {
				return outcomes, fmt.Errorf("subscription %d reported twice; %d distinct exits are required", o.index, want)
			}
			seen[o.index] = true
			outcomes = append(outcomes, o)
		case <-deadline:
			return outcomes, fmt.Errorf("%d of %d subscriptions returned within %s; still open: %v",
				len(outcomes), want, timeout, missingIndexes(seen, want))
		}
	}
	return outcomes, nil
}

func missingIndexes(seen map[int]bool, want int) []int {
	var missing []int
	for i := 0; i < want; i++ {
		if !seen[i] {
			missing = append(missing, i)
		}
	}
	return missing
}

// clientStackMarker identifies a goroutine inside one of this package's client methods, which is exactly
// what SubscribeWithContext starts.
const clientStackMarker = "sse.(*Client)."

// stackSample is one collector answer together with the evidence about whether the dump was whole.
type stackSample struct {
	bufferSize int
	used       int
	goroutines int
	blocks     []string
	complete   bool
	matching   int
}

func (s stackSample) topFrames() string {
	var out []string
	for _, b := range s.blocks {
		if !strings.Contains(b, clientStackMarker) {
			continue
		}
		lines := strings.Split(strings.TrimSpace(b), "\n")
		if len(lines) > 2 {
			lines = lines[1:3]
		}
		out = append(out, strings.Join(lines, " / "))
		if len(out) >= 4 {
			break
		}
	}
	return strings.Join(out, " | ")
}

// collectClientStacks starts at first bytes and doubles until the dump actually covers every goroutine of
// this process. Comparing the parsed blocks against runtime.NumGoroutine is the completeness test: a
// fixed buffer that merely fills up is not, and the earlier version silently parsed whatever it got.
func collectClientStacks(first int) stackSample {
	if first <= 0 {
		first = 1 << 10
	}
	const maxSize = 1 << 24
	for size := first; ; size *= 2 {
		sample := stackSnapshot(size)
		if sample.complete || size >= maxSize {
			return sample
		}
	}
}

// stackSnapshot takes one raw sample of every goroutine stack in this process and records whether it
// actually covered them all. A truncated dump is left marked incomplete rather than parsed.
func stackSnapshot(size int) stackSample {
	buf := make([]byte, size)
	n := runtime.Stack(buf, true)
	want := runtime.NumGoroutine()
	blocks := strings.Split(string(buf[:n]), "\n\n")
	return stackSample{
		bufferSize: size,
		used:       n,
		goroutines: want,
		blocks:     blocks,
		complete:   n < size && len(blocks) >= want,
		matching:   countBlocks(blocks, clientStackMarker),
	}
}

func countBlocks(blocks []string, marker string) int {
	count := 0
	for _, b := range blocks {
		if strings.Contains(b, marker) {
			count++
		}
	}
	return count
}

// runChild re-executes this test binary for the scenario alone. probe leaves one subscription open so
// the controls can prove the acceptance actually rejects a leak.
func runChild(t *testing.T, probe bool) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, os.Args[0],
		"-test.run=^TestSubscribeWithContextDone$", "-test.v", "-test.count=1", "-test.timeout=60s")
	cmd.Env = append(os.Environ(), childEnv+"=1")
	if probe {
		cmd.Env = append(cmd.Env, probeEnv+"=1")
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}
