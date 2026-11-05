package service

import (
	res "github.com/zhangzhe-ctrl/ani-resource-service/api/sandbox/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Resource versions predating the admission guard only record these policies.
// Reject new claims at the public boundary even during a mixed-version rollout.
func rejectUnsupportedSandboxEgress(intent *res.SandboxEgressIntent) error {
	if intent != nil && (intent.NetworkEgressPolicy == "allowlist" || intent.NetworkEgressPolicy == "deny_all") {
		return status.Error(codes.Unimplemented, "SANDBOX_EGRESS_ENFORCEMENT_UNSUPPORTED")
	}
	return nil
}
