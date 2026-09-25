// T09 接管验收测试：protoc-gen-go-redact 手写运行包 redact/v1 的公开契约。
//
// 生成的 *.pb.redact.go 只用到这一层的少量符号，HTTP 链路测试覆盖不到
// 流式包装器与自定义脱敏注册表；本文件把整张表面钉住，使得
// 「接管只改 import 字符串」的说法可以运行验证而不是靠 grep。
package redact_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	redact "go-wind-admin/pkg/localdeps/go-wind-toolkit/protoc-gen-go-redact/redact/v1"
)

// fakeRedactor 是实现 Redactor 的最小替身，记录被调用次数。
type fakeRedactor struct{ calls int }

func (f *fakeRedactor) Redact() { f.calls++ }

// notRedactor 故意不实现 Redactor。
type notRedactor struct{ v string }

func TestT09ApplyOnlyTouchesRedactors(t *testing.T) {
	got := &fakeRedactor{}
	redact.Apply(got)
	require.Equal(t, 1, got.calls, "Apply 必须调用 Redact() 一次")

	// Apply 对普通指针、nil 与值类型都必须静默无操作，不能 panic。
	plain := &notRedactor{v: "keep"}
	redact.Apply(plain)
	require.Equal(t, "keep", plain.v)
	require.NotPanics(t, func() { redact.Apply(nil) })
}

func TestT09BypassDefaultsToFalseAndWrapperOverrides(t *testing.T) {
	require.False(t, redact.Falsy.CheckInternal(context.Background()),
		"Falsy 必须恒为 false，生成的包装器因此总是执行脱敏")

	var seen []string
	w := redact.Wrapper(func(ctx context.Context) bool {
		seen = append(seen, "asked")
		return true
	})
	require.True(t, w.CheckInternal(context.Background()))
	require.Equal(t, []string{"asked"}, seen, "Wrapper 必须把调用转交自定义函数")

	var bypass redact.Bypass = w
	require.Implements(t, (*redact.Bypass)(nil), bypass)
}

// TestT09CustomRedactorRegistryIsSharedSingleton 全局注册表必须只有一个实例：
// 注册点与生成代码的读取点看到的是同一份 map。
func TestT09CustomRedactorRegistryIsSharedSingleton(t *testing.T) {
	const name = "t09-upper"
	require.Equal(t, "as-is", redact.ApplyCustomRedactor(name, "as-is"),
		"未注册名字必须原样返回，不能报错")

	redact.RegisterCustomRedactor(name, redact.CustomRedactor(func(s string) string {
		return "<" + s + ">"
	}))
	require.Equal(t, "<v>", redact.ApplyCustomRedactor(name, "v"))

	// 覆盖注册：后写入生效（同名规则只有一个来源）。
	redact.RegisterCustomRedactor(name, redact.CustomRedactor(func(s string) string { return "!" }))
	require.Equal(t, "!", redact.ApplyCustomRedactor(name, "v"))

	// 未注册名字返回原值；但已注册成 nil 函数会直接 panic —— 这是锁定上游
	// （v0.0.0-20260831125122-5bb4931991b2）的既有行为，T09 只接管不修语义，
	// 因此按现状钉住并登记为已知继承缺陷，见 migration/receipts/T09.json。
	redact.RegisterCustomRedactor("t09-nil-redactor", nil)
	require.Panics(t, func() { _ = redact.ApplyCustomRedactor("t09-nil-redactor", "v") },
		"上游对 nil 注册没有防护：如要改变该语义需单独授权")
	require.Equal(t, "untouched", redact.ApplyCustomRedactor("t09-never-registered", "untouched"))
}

// streamSendRecorder 记录 Send / SendAndClose 收到的消息。
type streamSendRecorder[Res any] struct {
	grpc.ServerStreamingServer[Res]
	sent []*Res
}

func (s *streamSendRecorder[Res]) Send(m *Res) error {
	s.sent = append(s.sent, m)
	return nil
}

type bidiRecorder[Req any, Res any] struct {
	grpc.BidiStreamingServer[Req, Res]
	sent []*Res
}

func (s *bidiRecorder[Req, Res]) Send(m *Res) error {
	s.sent = append(s.sent, m)
	return nil
}

type clientStreamRecorder[Req any, Res any] struct {
	grpc.ClientStreamingServer[Req, Res]
	sent []*Res
}

func (s *clientStreamRecorder[Req, Res]) SendAndClose(m *Res) error {
	s.sent = append(s.sent, m)
	return nil
}

// TestT09StreamRedactorsApplyBeforeForwarding 三种流式包装器都必须在转发前脱敏。
func TestT09StreamRedactorsApplyBeforeForwarding(t *testing.T) {
	serverStream := &streamSendRecorder[fakeRedactor]{}
	msg := &fakeRedactor{}
	require.NoError(t, (&redact.ServerStreamRedactor[fakeRedactor]{
		ServerStreamingServer: serverStream,
	}).Send(msg))
	require.Equal(t, 1, msg.calls, "ServerStreamRedactor.Send 必须先 Apply")
	require.Equal(t, []*fakeRedactor{msg}, serverStream.sent)

	bidiStream := &bidiRecorder[fakeRedactor, fakeRedactor]{}
	bidiMsg := &fakeRedactor{}
	require.NoError(t, (&redact.BidiStreamRedactor[fakeRedactor, fakeRedactor]{
		BidiStreamingServer: bidiStream,
	}).Send(bidiMsg))
	require.Equal(t, 1, bidiMsg.calls)
	require.Equal(t, []*fakeRedactor{bidiMsg}, bidiStream.sent)

	clientStream := &clientStreamRecorder[fakeRedactor, fakeRedactor]{}
	clientMsg := &fakeRedactor{}
	require.NoError(t, (&redact.ClientStreamRedactor[fakeRedactor, fakeRedactor]{
		ClientStreamingServer: clientStream,
	}).SendAndClose(clientMsg))
	require.Equal(t, 1, clientMsg.calls)
	require.Equal(t, []*fakeRedactor{clientMsg}, clientStream.sent)
}

// TestT09StreamRedactorsTolerateNonRedactors 流式包装器对普通消息也必须透传。
func TestT09StreamRedactorsTolerateNonRedactors(t *testing.T) {
	rec := &streamSendRecorder[notRedactor]{}
	require.NoError(t, (&redact.ServerStreamRedactor[notRedactor]{
		ServerStreamingServer: rec,
	}).Send(&notRedactor{v: "plain"}))
	require.Equal(t, "plain", rec.sent[0].v)
}
