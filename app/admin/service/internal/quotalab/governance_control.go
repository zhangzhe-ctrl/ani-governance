//go:build quota_lab

package quotalab

// Governance 侧仅 quota_lab 装配的控制监听（§11.6）：
// 通过独立 127.0.0.1 listener 接受任务脚本指令（worker 暂停屏障、
// ResumeDispatch）。正式构建不导入本实现、不注册控制路由。

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/service"
)

// GovernanceControl 治理侧任务控制监听。
type GovernanceControl struct {
	worker *service.QuotaDispatchWorker
	ledger *data.QuotaLedgerRepo
	audit  *sync.Map // operation_id -> 操作记录（审计）

	listener net.Listener
	server   *http.Server
	token    string
}

// NewGovernanceControl 启动控制监听；token 文件 0600。
func NewGovernanceControl(worker *service.QuotaDispatchWorker, ledger *data.QuotaLedgerRepo, addr, tokenFile string) (*GovernanceControl, error) {
	tokenRaw, err := os.ReadFile(tokenFile)
	if err != nil {
		return nil, fmt.Errorf("read control token: %w", err)
	}
	token := strings.TrimSpace(string(tokenRaw))
	if err = os.Chmod(tokenFile, 0o600); err != nil {
		return nil, err
	}
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	c := &GovernanceControl{worker: worker, ledger: ledger, listener: lis, token: token, audit: &sync.Map{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/control/worker/pause", c.auth(c.workerPause))
	mux.HandleFunc("/control/dispatch/resume", c.auth(c.dispatchResume))
	mux.HandleFunc("/control/healthz", c.auth(c.healthz))
	c.server = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	return c, nil
}

func (c *GovernanceControl) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Control-Token") != c.token {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func (c *GovernanceControl) writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// workerPause 设置占额提交后暂停投递屏障（FAIL-02/15）。
func (c *GovernanceControl) workerPause(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Paused bool `json:"paused"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	c.worker.SetPaused(req.Paused)
	c.writeJSON(w, map[string]bool{"paused": c.worker.Paused()})
}

// dispatchResume 清除 retry_blocked（FAIL-17），保留原 operation/charge/request_hash；
// 记录审计，不允许改账本余额。
func (c *GovernanceControl) dispatchResume(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OperationID string `json:"operation_id"`
		TenantID    uint32 `json:"tenant_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if req.OperationID == "" || req.TenantID == 0 {
		http.Error(w, "operation_id and tenant_id required", 400)
		return
	}
	if err := c.ledger.ResumeDispatch(r.Context(), req.TenantID, req.OperationID); err != nil {
		c.audit.Store(req.OperationID, fmt.Sprintf("resume failed: %v at %s", err, time.Now().Format(time.RFC3339)))
		http.Error(w, err.Error(), 500)
		return
	}
	c.audit.Store(req.OperationID, fmt.Sprintf("resumed at %s", time.Now().Format(time.RFC3339)))
	c.writeJSON(w, map[string]string{"operation_id": req.OperationID, "status": "resumed"})
}

func (c *GovernanceControl) healthz(w http.ResponseWriter, r *http.Request) {
	c.writeJSON(w, map[string]interface{}{"paused": c.worker.Paused(), "time": time.Now().Format(time.RFC3339)})
}

// Addr 返回监听地址。
func (c *GovernanceControl) Addr() string { return c.listener.Addr().String() }

// Start 启动。
func (c *GovernanceControl) Start() error {
	go func() { _ = c.server.Serve(c.listener) }()
	return nil
}

// Stop 停止。
func (c *GovernanceControl) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = c.server.Shutdown(ctx)
}
