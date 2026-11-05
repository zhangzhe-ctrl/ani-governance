package service

import (
	"testing"

	res "github.com/zhangzhe-ctrl/ani-resource-service/api/sandbox/v1"
	"google.golang.org/protobuf/proto"
)

func TestSandboxMetricsHistoryFenceIgnoresOrderingButRejectsMutation(t *testing.T) {
	first := &res.ListSandboxPodHistoryResponse{Pods: []*res.SandboxPodHistory{
		{PodUid: "11111111-1111-4111-8111-111111111111", PodName: "one"},
		{PodUid: "22222222-2222-4222-8222-222222222222", PodName: "two"},
	}}
	last := proto.Clone(first).(*res.ListSandboxPodHistoryResponse)
	last.Pods[0], last.Pods[1] = last.Pods[1], last.Pods[0]
	if !sameSandboxPodHistory(first, last) {
		t.Fatal("reordered unchanged history was rejected")
	}
	last.Pods[0].PodName = "changed"
	if sameSandboxPodHistory(first, last) {
		t.Fatal("changed Pod identity accepted")
	}
	last = proto.Clone(first).(*res.ListSandboxPodHistoryResponse)
	last.Pods = append(last.Pods, &res.SandboxPodHistory{PodUid: "33333333-3333-4333-8333-333333333333"})
	if sameSandboxPodHistory(first, last) {
		t.Fatal("newly recorded Pod UID accepted")
	}
	last = proto.Clone(first).(*res.ListSandboxPodHistoryResponse)
	last.Pods[1] = last.Pods[0]
	if sameSandboxPodHistory(first, last) {
		t.Fatal("duplicate Pod identity accepted")
	}
	last = proto.Clone(first).(*res.ListSandboxPodHistoryResponse)
	last.HistoryCompleteBeforeMigration = !last.HistoryCompleteBeforeMigration
	if sameSandboxPodHistory(first, last) {
		t.Fatal("history completeness change accepted")
	}
}
