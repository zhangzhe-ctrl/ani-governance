package service

import (
	"context"
	"google.golang.org/protobuf/proto"
	"net/netip"
	"strconv"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"
	res "github.com/zhangzhe-ctrl/ani-resource-service/api/sandbox/v1"
	view "go-wind-admin/api/gen/go/sandbox/service/v1"
	"go-wind-admin/app/admin/service/internal/data"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metavalidation "k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	utilvalidation "k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

type sandboxEgressClient interface {
	GetSandboxEgress(context.Context, string, string, *res.GetSandboxEgressRequest) (*res.GetSandboxEgressResponse, error)
}

// GetSandboxEgress returns only the verified Kubernetes policy declaration.
// The network plugin's enforcement and any Host allowlist are not inferred.
func (s *SandboxService) GetSandboxEgress(ctx context.Context, r *view.GetSandboxEgressRequest) (*view.GetSandboxEgressResponse, error) {
	if r == nil || !sandboxUUID(r.SandboxId) {
		return nil, kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "invalid sandbox identity")
	}
	tenant, actor, err := s.scope(ctx, data.SandboxEgressPath, "GET")
	if err != nil {
		return nil, err
	}
	client, ok := s.client.(sandboxEgressClient)
	if !ok {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_UNAVAILABLE", "sandbox egress is not configured")
	}
	out, err := client.GetSandboxEgress(ctx, tenant, actor, &res.GetSandboxEgressRequest{TenantId: tenant, SandboxId: r.SandboxId})
	if err != nil {
		return nil, err
	}
	invalid := func() (*view.GetSandboxEgressResponse, error) {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_INVALID_RESPONSE", "invalid sandbox egress declaration")
	}
	if out == nil || out.Enforcement != "unknown" {
		return invalid()
	}
	var intent *view.SandboxEgressIntent
	if out.Intent == nil {
		if out.HostAllowlistKnown || out.HostAllowlistStatus != "unknown" || out.IntentEnforcement != "" {
			return invalid()
		}
	} else {
		normalized, err := res.NormalizeEgressIntent(out.Intent)
		if err != nil || !proto.Equal(normalized, out.Intent) || out.IntentEnforcement != "not_applied_by_sandbox" {
			return invalid()
		}
		if out.Intent.NetworkEgressPolicy == "allowlist" {
			if !out.HostAllowlistKnown || out.HostAllowlistStatus != "known" {
				return invalid()
			}
		} else if out.HostAllowlistKnown || out.HostAllowlistStatus != "not_applicable" {
			return invalid()
		}
		intent = &view.SandboxEgressIntent{NetworkEgressPolicy: out.Intent.NetworkEgressPolicy, EgressAllowlist: append([]string{}, out.Intent.EgressAllowlist...)}
	}
	if out.Source == "unavailable" {
		if out.Declaration != "unknown" || out.Isolated || len(out.Rules) != 0 || intent == nil {
			return invalid()
		}
		return &view.GetSandboxEgressResponse{Source: out.Source, Declaration: out.Declaration, Enforcement: out.Enforcement, HostAllowlistKnown: out.HostAllowlistKnown, HostAllowlistStatus: out.HostAllowlistStatus, Intent: intent, IntentEnforcement: out.IntentEnforcement, Rules: []*view.SandboxEgressRule{}}, nil
	}
	if out.Source != "kubernetes_network_policy" {
		return invalid()
	}
	switch out.Declaration {
	case "unrestricted":
		if !out.Isolated && len(out.Rules) != 0 {
			return invalid()
		}
		if out.Isolated {
			allowAll := false
			for _, rule := range out.Rules {
				allowAll = allowAll || rule != nil && len(rule.Peers) == 0 && len(rule.Ports) == 0
			}
			if !allowAll {
				return invalid()
			}
		}
	case "deny_all":
		if !out.Isolated || len(out.Rules) != 0 {
			return invalid()
		}
	case "restricted":
		if !out.Isolated || len(out.Rules) == 0 {
			return invalid()
		}
		for _, rule := range out.Rules {
			if rule != nil && len(rule.Peers) == 0 && len(rule.Ports) == 0 {
				return invalid()
			}
		}
	default:
		return invalid()
	}
	result := &view.GetSandboxEgressResponse{Source: out.Source, Declaration: out.Declaration, Enforcement: out.Enforcement, HostAllowlistKnown: out.HostAllowlistKnown, HostAllowlistStatus: out.HostAllowlistStatus, Isolated: out.Isolated, Intent: intent, IntentEnforcement: out.IntentEnforcement, Rules: []*view.SandboxEgressRule{}}
	for _, rule := range out.Rules {
		if rule == nil {
			return invalid()
		}
		mapped := &view.SandboxEgressRule{Peers: []*view.SandboxEgressPeer{}, Ports: []*view.SandboxEgressPort{}}
		for _, peer := range rule.Peers {
			if !validSandboxEgressPeer(peer) {
				return invalid()
			}
			pod, ok := sandboxEgressSelector(peer.PodSelector)
			if !ok {
				return invalid()
			}
			namespace, ok := sandboxEgressSelector(peer.NamespaceSelector)
			if !ok {
				return invalid()
			}
			dest := &view.SandboxEgressPeer{PodSelector: pod, NamespaceSelector: namespace}
			if peer.IpBlock != nil {
				dest.IpBlock = &view.SandboxEgressIPBlock{Cidr: peer.IpBlock.Cidr, Except: append([]string{}, peer.IpBlock.Except...)}
			}
			mapped.Peers = append(mapped.Peers, dest)
		}
		for _, port := range rule.Ports {
			if !validSandboxEgressPort(port) {
				return invalid()
			}
			mapped.Ports = append(mapped.Ports, &view.SandboxEgressPort{Protocol: port.Protocol, PortType: port.PortType, Port: port.Port, EndPort: port.EndPort})
		}
		result.Rules = append(result.Rules, mapped)
	}
	return result, nil
}

func sandboxEgressSelector(src *res.SandboxEgressSelector) (*view.SandboxEgressSelector, bool) {
	if src == nil {
		return nil, true
	}
	out := &view.SandboxEgressSelector{MatchLabels: make(map[string]string, len(src.MatchLabels)), MatchExpressions: []*view.SandboxEgressLabelExpression{}}
	for k, v := range src.MatchLabels {
		out.MatchLabels[k] = v
	}
	for _, expr := range src.MatchExpressions {
		if expr == nil {
			return nil, false
		}
		out.MatchExpressions = append(out.MatchExpressions, &view.SandboxEgressLabelExpression{Key: expr.Key, Operator: expr.Operator, Values: append([]string{}, expr.Values...)})
	}
	return out, true
}

func validSandboxEgressPeer(peer *res.SandboxEgressPeer) bool {
	if peer == nil || peer.PodSelector == nil && peer.NamespaceSelector == nil && peer.IpBlock == nil {
		return false
	}
	if peer.IpBlock != nil {
		if peer.PodSelector != nil || peer.NamespaceSelector != nil {
			return false
		}
		cidr, err := netip.ParsePrefix(peer.IpBlock.Cidr)
		if err != nil {
			return false
		}
		for _, raw := range peer.IpBlock.Except {
			except, err := netip.ParsePrefix(raw)
			if err != nil || except.Bits() < cidr.Bits() || !cidr.Contains(except.Addr()) {
				return false
			}
		}
	}
	return validSandboxEgressSelector(peer.PodSelector) && validSandboxEgressSelector(peer.NamespaceSelector)
}

func validSandboxEgressSelector(src *res.SandboxEgressSelector) bool {
	if src == nil {
		return true
	}
	selector := &metav1.LabelSelector{MatchLabels: src.MatchLabels}
	for _, expr := range src.MatchExpressions {
		if expr == nil {
			return false
		}
		selector.MatchExpressions = append(selector.MatchExpressions, metav1.LabelSelectorRequirement{Key: expr.Key, Operator: metav1.LabelSelectorOperator(expr.Operator), Values: expr.Values})
	}
	return len(metavalidation.ValidateLabelSelector(selector, metavalidation.LabelSelectorValidationOptions{}, field.NewPath("selector"))) == 0
}

func validSandboxEgressPort(port *res.SandboxEgressPort) bool {
	if port == nil || port.Protocol != "TCP" && port.Protocol != "UDP" && port.Protocol != "SCTP" {
		return false
	}
	switch port.PortType {
	case "all":
		return port.Port == "" && port.EndPort == 0
	case "name":
		return port.EndPort == 0 && len(utilvalidation.IsValidPortName(port.Port)) == 0
	case "number":
		n, err := strconv.Atoi(port.Port)
		return err == nil && strconv.Itoa(n) == port.Port && n >= 1 && n <= 65535 && (port.EndPort == 0 || port.EndPort >= int32(n) && port.EndPort <= 65535)
	default:
		return false
	}
}
