//go:build quota_lab

package simulator

// 故障控制面（§11.6）：独立 127.0.0.1 listener，任务随机控制 token（0600）。
// 显式 barrier/ack，不依赖 sleep 猜时序。不放到公网用户 API。

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
)

// ControlPlane 模拟器故障控制面。
type ControlPlane struct {
	sim *Simulator

	mu           sync.Mutex
	createFail   int
	releaseFail  int
	blockNotify  bool
	listener     net.Listener
	server       *http.Server
	token        string
}

// NewControlPlane 启动控制面（127.0.0.1 only）。token 文件 0600。
func NewControlPlane(sim *Simulator, addr, tokenFile string) (*ControlPlane, error) {
	token, err := os.ReadFile(tokenFile)
	if err != nil {
		return nil, fmt.Errorf("read control token: %w", err)
	}
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	if err = os.Chmod(tokenFile, 0o600); err != nil {
		return nil, err
	}
	cp := &ControlPlane{sim: sim, listener: lis, token: trimSpace(string(token))}

	mux := http.NewServeMux()
	mux.HandleFunc("/control/fail-create-ordinal", cp.auth(cp.setFailCreate))
	mux.HandleFunc("/control/fail-release-ordinal", cp.auth(cp.setFailRelease))
	mux.HandleFunc("/control/block-release", cp.auth(cp.setBlockNotify))
	mux.HandleFunc("/control/state", cp.auth(cp.state))
	cp.server = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	return cp, nil
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == '\n' || s[start] == ' ' || s[start] == '\r' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == '\n' || s[end-1] == ' ' || s[end-1] == '\r' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

func (c *ControlPlane) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Control-Token") != c.token {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func (c *ControlPlane) writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (c *ControlPlane) setFailCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Ordinal int `json:"ordinal"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	c.mu.Lock()
	c.createFail = req.Ordinal
	c.sim.Fails.FailCreateOrdinal = req.Ordinal
	c.mu.Unlock()
	c.writeJSON(w, map[string]int{"fail_create_ordinal": req.Ordinal})
}

func (c *ControlPlane) setFailRelease(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Ordinal int `json:"ordinal"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	c.mu.Lock()
	c.releaseFail = req.Ordinal
	c.sim.Fails.FailReleaseOrdinal = req.Ordinal
	c.mu.Unlock()
	c.writeJSON(w, map[string]int{"fail_release_ordinal": req.Ordinal})
}

func (c *ControlPlane) setBlockNotify(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Blocked bool `json:"blocked"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	c.mu.Lock()
	c.blockNotify = req.Blocked
	c.sim.Fails.BlockReleaseNotify = req.Blocked
	c.mu.Unlock()
	c.writeJSON(w, map[string]bool{"blocked": req.Blocked})
}

// state 输出内部完整事实（任务证据用只读 SQL 等价物）。
func (c *ControlPlane) state(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := map[string]interface{}{}
	var facts, pending, delivered int
	_ = c.sim.owner.db.QueryRowContext(ctx, `SELECT count(*) FROM sim_release_facts`).Scan(&facts)
	_ = c.sim.owner.db.QueryRowContext(ctx, `SELECT count(*) FROM sim_notify_queue WHERE state='pending'`).Scan(&pending)
	_ = c.sim.owner.db.QueryRowContext(ctx, `SELECT count(*) FROM sim_notify_queue WHERE state='delivered'`).Scan(&delivered)
	var allocated int
	_ = c.sim.provider.db.QueryRowContext(ctx, `SELECT count(*) FROM sim_allocations WHERE state='allocated'`).Scan(&allocated)
	c.mu.Lock()
	out["fail_create_ordinal"] = c.createFail
	out["fail_release_ordinal"] = c.releaseFail
	out["release_notify_blocked"] = c.blockNotify
	c.mu.Unlock()
	out["release_facts"] = facts
	out["notify_pending"] = pending
	out["notify_delivered"] = delivered
	out["provider_allocated_units"] = allocated
	c.writeJSON(w, out)
}

// Addr 返回监听地址。
func (c *ControlPlane) Addr() string { return c.listener.Addr().String() }

// Start 启动。
func (c *ControlPlane) Start() error {
	go func() { _ = c.server.Serve(c.listener) }()
	return nil
}

// Stop 停止。
func (c *ControlPlane) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = c.server.Shutdown(ctx)
}
