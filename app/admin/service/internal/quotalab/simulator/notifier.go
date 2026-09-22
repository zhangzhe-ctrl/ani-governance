//go:build quota_lab

package simulator

// 可靠退额通知 worker（§11.5）：owner 将累计释放事实与待发通知同事务提交；
// 本 worker 负责把待发通知经内部 mTLS 发给 Governance 的 QuotaReleaseService，
// 失败保留通知，重启继续发送；成功 ACK 后标 delivered。不使用 MQ。

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	quotapb "go-wind-admin/api/gen/go/quota/service/v1"
)

// NotifierConfig 退额通知配置。
type NotifierConfig struct {
	GovernanceAddr string // Governance 内部退额 listener（QUOTA-03）
	CAFile         string
	CertFile       string
	KeyFile        string
	Interval       time.Duration
}

// Notifier 待发退额通知发送器。
type Notifier struct {
	store  *OwnerStore
	client quotapb.QuotaReleaseServiceClient
	cfg    NotifierConfig
	stopCh chan struct{}
	done   chan struct{}
}

// NewNotifier 构造 mTLS 客户端；证书 SAN 必须精确为 ani-gpu-simulator。
func NewNotifier(store *OwnerStore, cfg NotifierConfig) (*Notifier, error) {
	if cfg.GovernanceAddr == "" {
		return nil, fmt.Errorf("governance release address is required")
	}
	if cfg.Interval <= 0 {
		cfg.Interval = time.Second
	}
	pem, err := os.ReadFile(cfg.CAFile)
	if err != nil {
		return nil, fmt.Errorf("read release CA: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("release CA contains no certificates")
	}
	cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load simulator client certificate: %w", err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return nil, err
	}
	trusted := false
	for _, name := range leaf.DNSNames {
		if name == "ani-gpu-simulator" {
			trusted = true
		}
	}
	if !trusted {
		return nil, fmt.Errorf("simulator certificate requires exact DNS SAN ani-gpu-simulator")
	}
	conn, err := grpc.NewClient(cfg.GovernanceAddr,
		grpc.WithDisableServiceConfig(),
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			MinVersion:   tls.VersionTLS13,
			RootCAs:      roots,
			Certificates: []tls.Certificate{cert},
			ServerName:   "ani-governance",
		})))
	if err != nil {
		return nil, err
	}
	return &Notifier{
		store:  store,
		client: quotapb.NewQuotaReleaseServiceClient(conn),
		cfg:    cfg,
		stopCh: make(chan struct{}),
		done:   make(chan struct{}),
	}, nil
}

// Start 启动轮询。
func (n *Notifier) Start() {
	go n.loop()
}

// Stop 停止。
func (n *Notifier) Stop() {
	close(n.stopCh)
	select {
	case <-n.done:
	case <-time.After(5 * time.Second):
	}
}

func (n *Notifier) loop() {
	defer close(n.done)
	ticker := time.NewTicker(n.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-n.stopCh:
			return
		case <-ticker.C:
		}
		_ = n.DeliverPending(context.Background())
	}
}

type pendingNotify struct {
	EventID     string
	OperationID string
	ChargeID    string
	TenantID    string
	PayloadHash string
	PayloadJSON string
	Reason      string
}

// DeliverPending 发送全部待发通知；失败保留，下次继续（重复发送合法，
// Governance 幂等）。
func (n *Notifier) DeliverPending(ctx context.Context) error {
	rows, err := n.store.db.QueryContext(ctx,
		`SELECT event_id, operation_id, charge_id, tenant_id, payload_hash, payload_json, reason
		 FROM sim_notify_queue WHERE state='pending' ORDER BY created_at LIMIT 16`)
	if err != nil {
		return err
	}
	var pending []pendingNotify
	for rows.Next() {
		var p pendingNotify
		if err = rows.Scan(&p.EventID, &p.OperationID, &p.ChargeID, &p.TenantID, &p.PayloadHash, &p.PayloadJSON, &p.Reason); err != nil {
			_ = rows.Close()
			return err
		}
		pending = append(pending, p)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()

	for _, p := range pending {
		if err = n.deliverOne(ctx, p); err != nil {
			_, _ = n.store.db.ExecContext(ctx,
				`UPDATE sim_notify_queue SET attempt=attempt+1, updated_at=now() WHERE event_id=$1`, p.EventID)
			continue // 保留通知，重试
		}
	}
	return nil
}

func (n *Notifier) deliverOne(ctx context.Context, p pendingNotify) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	reason := quotapb.ReleaseReason_RESOURCE_RELEASED
	if p.Reason == "ABORTED_CLEANED" {
		reason = quotapb.ReleaseReason_ABORTED_CLEANED
	}

	// 从 payload 解析累计释放量（入队时由唯一释放事实求和得到）。
	releasedTotal, err := extractReleasedTotal(p.PayloadJSON)
	if err != nil {
		return err
	}
	// release_event_id 必须为 UUID；非 UUID 的内部 event id 转换为稳定 UUID。
	eventID := p.EventID
	if _, err = uuid.Parse(eventID); err != nil {
		eventID = uuid.NewSHA1(uuid.NameSpaceURL, []byte(eventID)).String()
	}

	_, err = n.client.ReportQuotaRelease(ctx, &quotapb.ReportQuotaReleaseRequest{
		ReleaseEventId: eventID,
		OperationId:    p.OperationID,
		Items: []*quotapb.QuotaReleaseItem{{
			ChargeId:      p.ChargeID,
			QuotaCode:     "gpu.count",
			ReleasedTotal: releasedTotal,
		}},
		Reason: reason,
	})
	if err != nil {
		return err
	}
	_, err = n.store.db.ExecContext(context.Background(),
		`UPDATE sim_notify_queue SET state='delivered', updated_at=now() WHERE event_id=$1`, p.EventID)
	return err
}

// extractReleasedTotal 从固定格式 payload 中取 released_total。
func extractReleasedTotal(payload string) (int64, error) {
	needle := `"released_total":`
	idx := -1
	for i := 0; i+len(needle) <= len(payload); i++ {
		if payload[i:i+len(needle)] == needle {
			idx = i + len(needle)
			break
		}
	}
	if idx < 0 {
		return 0, fmt.Errorf("payload missing released_total")
	}
	rest := payload[idx:]
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, fmt.Errorf("payload released_total malformed")
	}
	var v int64
	if _, err := fmt.Sscanf(rest[:end], "%d", &v); err != nil {
		return 0, err
	}
	return v, nil
}

var _ = sql.ErrNoRows
