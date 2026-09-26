package sse

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStreamAddSubscriber keeps the upstream scenario: autoReplay is on, the first event is
// published before the subscriber registers and the second one after it.
//
// The upstream body read exactly one event and then asserted that the subscriber's queue was empty
// (stream_test.go:26, "expected: 0, actual: 1"). That assertion is not a property of the stream: with
// autoReplay the pre-registration event is either delivered directly or replayed from the log, so the
// subscriber legitimately receives both events, and whether the second one had already been dispatched
// when len() was sampled is pure timing (measured ~1 failure in 8 whole-package runs).
//
// This version therefore consumes both messages and asserts what the contract actually promises: both
// arrive, in publication order, as two distinct logged events, with nothing left in the queue; then
// the stream is closed and the shutdown is observed through the connection rather than assumed from
// the return of close(), which only signals quit.
func TestStreamAddSubscriber(t *testing.T) {
	s := newStream("test", 1024, true, false, nil, nil)
	s.run()
	defer s.close()

	const firstPayload, secondPayload = "before-subscribe", "after-subscribe"

	s.event <- &Event{Data: []byte(firstPayload)}
	sub := s.addSubscriber("", nil)

	assert.Equal(t, 1, s.getSubscriberCount())

	s.event <- &Event{Data: []byte(secondPayload)}

	var received, ids []string
	for len(received) < 2 {
		ev, err := waitEvent(sub.connection, time.Second*3)
		require.Nil(t, err, "订阅者应在期限内收到第 %d 条消息", len(received)+1)
		require.NotNil(t, ev)
		received = append(received, string(ev.Data))
		ids = append(ids, string(ev.ID))
	}
	assert.Equal(t, []string{firstPayload, secondPayload}, received,
		"两条消息都应送达，且顺序与发布顺序一致")
	assert.NotEmpty(t, ids[0], "被重放的先发事件也应带事件号")
	assert.NotEmpty(t, ids[1], "订阅后的事件也应带事件号")
	assert.NotEqual(t, ids[0], ids[1], "收到的应是两个不同事件，而不是同一事件被投递两次")

	// 原断言保留在这里：两条都取走之后，队列必须为空。
	assert.Equal(t, 0, len(sub.connection), "两条消息取完后不应仍有排队事件")

	// 关闭流；connection 被关闭才算收尾完成。close() 只发送 quit，返回不等于事件循环已退出，
	// 因此这里以观察通道关闭为准，并保留"没有额外消息"的检查。
	s.close()
	select {
	case ev, ok := <-sub.connection:
		if ok {
			t.Errorf("关闭流之后仍收到额外消息: data=%q id=%q", string(ev.Data), string(ev.ID))
		}
	case <-time.After(5 * time.Second):
		t.Error("s.close() 之后订阅连接在 5s 内未关闭")
	}
	assert.Equal(t, 0, len(sub.connection), "关闭后订阅连接中不应残留事件")
}

func TestStreamRemoveSubscriber(t *testing.T) {
	s := newStream("test", 1024, true, false, nil, nil)
	s.run()
	defer s.close()

	sub := s.addSubscriber("", nil)
	time.Sleep(time.Millisecond * 100)
	s.deregister <- sub
	time.Sleep(time.Millisecond * 100)

	assert.Equal(t, 0, s.getSubscriberCount())
}

func TestStreamSubscriberClose(t *testing.T) {
	s := newStream("test", 1024, true, false, nil, nil)
	s.run()
	defer s.close()

	sub := s.addSubscriber("", nil)
	sub.close()
	time.Sleep(time.Millisecond * 100)

	assert.Equal(t, 0, s.getSubscriberCount())
}

func TestStreamDisableAutoReplay(t *testing.T) {
	s := newStream("test", 1024, true, false, nil, nil)
	s.run()
	defer s.close()

	s.autoReplay = false
	s.event <- &Event{Data: []byte("test")}
	time.Sleep(time.Millisecond * 100)
	sub := s.addSubscriber("", nil)

	assert.Equal(t, 0, len(sub.connection))
}

func TestStreamMultipleSubscribers(t *testing.T) {
	var subs []*Subscriber

	s := newStream("test", 1024, true, false, nil, nil)
	s.run()

	for i := 0; i < 10; i++ {
		subs = append(subs, s.addSubscriber("", nil))
	}

	// Wait for all subscribers to be added
	time.Sleep(time.Millisecond * 100)

	s.event <- &Event{Data: []byte("test")}
	for _, sub := range subs {
		msg, err := wait(sub.connection, time.Second*1)
		require.Nil(t, err)
		assert.Equal(t, []byte(`test`), msg)
	}

	s.close()

	// Wait for all subscribers to close
	time.Sleep(time.Millisecond * 100)
	assert.Equal(t, 0, s.getSubscriberCount())

}
