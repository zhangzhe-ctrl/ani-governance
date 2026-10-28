package service

import (
	"context"
	"fmt"
	"testing"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/stretchr/testify/require"
	res "github.com/zhangzhe-ctrl/ani-resource-service/api/sandbox/v1"
)

type sandboxChildPages struct {
	SandboxOwnerClient
	pages map[uint32]*res.ListSandboxesResponse
	err   error
}

func (p sandboxChildPages) ListSandboxes(_ context.Context, _, _ string, r *res.ListSandboxesRequest) (*res.ListSandboxesResponse, error) {
	return p.pages[r.Page], p.err
}
func TestSandboxParentDeleteChecksAllPages(t *testing.T) {
	tenant, instance := "tenant-a", "parent-a"
	first := &res.ListSandboxesResponse{Total: 101}
	for i := 0; i < 100; i++ {
		first.Items = append(first.Items, &res.Sandbox{Id: fmt.Sprint(i), TenantId: tenant, InstanceId: instance, DesiredState: "deleted", ObservedState: "deleted"})
	}
	last := &res.Sandbox{Id: "last", TenantId: tenant, InstanceId: instance, DesiredState: "deleted", ObservedState: "deleted"}
	pages := sandboxChildPages{pages: map[uint32]*res.ListSandboxesResponse{1: first, 2: {Total: 101, Items: []*res.Sandbox{last}}}}
	require.NoError(t, checkSandboxChildrenDeleted(context.Background(), pages, tenant, "actor", instance))
	last.ObservedState = "unknown"
	require.Equal(t, "INSTANCE_SANDBOX_NOT_DELETED", kratoserrors.Reason(checkSandboxChildrenDeleted(context.Background(), pages, tenant, "actor", instance)))
	last.ObservedState = "deleted"
	last.TenantId = "other"
	require.Equal(t, "SANDBOX_CHECK_UNAVAILABLE", kratoserrors.Reason(checkSandboxChildrenDeleted(context.Background(), pages, tenant, "actor", instance)))
	last.TenantId = tenant
	pages.pages[2].Total = 102
	require.Equal(t, "SANDBOX_CHECK_UNAVAILABLE", kratoserrors.Reason(checkSandboxChildrenDeleted(context.Background(), pages, tenant, "actor", instance)))
}
