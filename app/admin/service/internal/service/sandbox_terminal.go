package service

import (
	"context"
	"net/url"
	"strings"
	"time"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/google/uuid"
	terminal "github.com/zhangzhe-ctrl/ani-resource-service/api/sandbox/terminal/v1"
	session "github.com/zhangzhe-ctrl/ani-session-gateway/api/gen/session/v1"
	view "go-wind-admin/api/gen/go/sandbox/service/v1"
	"go-wind-admin/app/admin/service/internal/data"
)

type sandboxTerminalResolver interface {
	ResolveSandboxTerminal(context.Context, string, string, string) (*terminal.SandboxTerminalBinding, error)
}

// ExecSandbox creates a short-lived session for the independently identified
// Sandbox. Session Gateway revalidates the binding before it opens the stream.
func (s *SandboxService) ExecSandbox(ctx context.Context, r *view.ExecSandboxRequest) (*view.ExecSandboxResponse, error) {
	if r == nil || r.Data == nil {
		return nil, kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "terminal data required")
	}
	d := r.Data
	if !sandboxUUID(d.SandboxId) || !sandboxUUID(d.IdempotencyKey) || d.ExpectedVersion < 1 || d.Rows < 1 || d.Rows > 4096 || d.Cols < 1 || d.Cols > 4096 {
		return nil, kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "invalid terminal request")
	}
	tenant, actor, err := s.scope(ctx, data.SandboxExecPath, "POST")
	if err != nil {
		return nil, err
	}
	resolver, ok := s.client.(sandboxTerminalResolver)
	if !ok || s.sessions == nil {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_TERMINAL_UNAVAILABLE", "sandbox terminal unavailable")
	}
	binding, err := resolver.ResolveSandboxTerminal(ctx, tenant, actor, d.SandboxId)
	if err != nil {
		return nil, err
	}
	if binding == nil || binding.TenantId != tenant || binding.SandboxId != d.SandboxId || binding.Version != d.ExpectedVersion ||
		!sandboxUUID(binding.ClusterId) || binding.Namespace != "tenant-"+tenant || binding.NamespaceUid == "" ||
		binding.PodName != "sandbox-"+d.SandboxId || binding.PodUid == "" || binding.ContainerName != "sandbox" || binding.Shell != "/bin/sh" {
		return nil, kratoserrors.Conflict("SANDBOX_TERMINAL_NOT_READY", "sandbox terminal target changed")
	}
	reply, err := s.sessions.CreateSession(ctx, &session.CreateSessionRequest{
		// Session Gateway stores keys globally. Scope the user's key to this
		// tenant, actor and sandbox so unrelated callers cannot collide.
		RequestId: uuid.NewString(), IdempotencyKey: uuid.NewSHA1(uuid.NameSpaceURL, []byte("ani-governance/sandbox-terminal/"+tenant+"/"+actor+"/"+d.SandboxId+"/"+d.IdempotencyKey)).String(),
		Principal: &session.Principal{TenantId: tenant, SubjectId: actor},
		Target:    &session.Target{WorkloadName: binding.PodName, WorkloadKind: session.WorkloadKind_WORKLOAD_KIND_SANDBOX, SandboxBinding: binding},
		Mode:      &session.CreateSessionRequest_Exec{Exec: &session.ExecOptions{Container: binding.ContainerName, Command: []string{binding.Shell}, Tty: true, Rows: d.Rows, Cols: d.Cols}},
	})
	if err != nil {
		return nil, err
	}
	if reply == nil || !sandboxUUID(reply.SessionId) || reply.ExpiresAt == nil || reply.ExpiresAt.CheckValid() != nil || !reply.ExpiresAt.AsTime().After(time.Now()) || !validSandboxTerminalURL(reply.ConnectUrl, reply.SessionId) {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_TERMINAL_UNAVAILABLE", "invalid terminal response")
	}
	return &view.ExecSandboxResponse{SessionId: reply.SessionId, WsUrl: reply.ConnectUrl, ExpiresAt: reply.ExpiresAt.AsTime().UTC().Format(time.RFC3339Nano)}, nil
}

func validSandboxTerminalURL(raw, sessionID string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "ws" && u.Scheme != "wss") || u.Hostname() == "" || u.User != nil || u.Fragment != "" || !strings.HasSuffix(u.EscapedPath(), "/sessions/"+url.PathEscape(sessionID)) {
		return false
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return false
	}
	tickets := query["ticket"]
	return len(tickets) == 1 && tickets[0] != ""
}
