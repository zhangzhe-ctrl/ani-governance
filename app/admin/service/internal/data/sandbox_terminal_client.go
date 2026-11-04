package data

import (
	"context"

	terminal "github.com/zhangzhe-ctrl/ani-resource-service/api/sandbox/terminal/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ResolveSandboxTerminal obtains a point-in-time, tenant-scoped binding on the
// existing authenticated Resource connection. The gateway validates it again
// before opening a session.
func (c *InstanceClient) ResolveSandboxTerminal(ctx context.Context, tenant, actor, sandboxID string) (*terminal.SandboxTerminalBinding, error) {
	if c == nil || c.sandboxTerminal == nil {
		return nil, status.Error(codes.Unavailable, "SANDBOX_TERMINAL_UNAVAILABLE")
	}
	if !validResourceIdentity(tenant, actor) {
		return nil, status.Error(codes.Unauthenticated, "INVALID_SANDBOX_CALLER")
	}
	return sandboxCall(c, ctx, tenant, actor, func(call context.Context) (*terminal.SandboxTerminalBinding, error) {
		return c.sandboxTerminal.ResolveTerminalSession(call, &terminal.ResolveTerminalSessionRequest{TenantId: tenant, SandboxId: sandboxID})
	})
}
