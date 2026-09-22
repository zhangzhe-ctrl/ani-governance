//go:build quota_lab

// quota-gpu-simulator：独立进程的 GPU 模拟资源服务（QUOTA-GPU-LOCAL-01，§11）。
// 仅 quota_lab 构建；owner/provider 使用独立 PostgreSQL 数据库。
//
// 环境变量：
//   SIM_OWNER_DSN              owner 数据库
//   SIM_PROVIDER_DSN           provider 数据库
//   SIM_LISTEN_ADDR            gRPC 监听（127.0.0.1）
//   SIM_TLS_CA_FILE/_CERT/_KEY mTLS 材料（服务证书 SAN=ani-gpu-simulator）
//   SIM_CONTROL_ADDR           控制面监听（127.0.0.1）
//   SIM_CONTROL_TOKEN_FILE     控制面 token（0600）
//   ANI_QUOTA_INTERNAL_ADDR    Governance 内部退额 listener（QUOTA-03）
//   ANI_QUOTA_CA_FILE/_CERT/_KEY 退额 mTLS 材料（客户端证书 SAN=ani-gpu-simulator）
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // database/sql pgx 驱动

	"go-wind-admin/app/admin/service/internal/quotalab/simulator"
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "quota-gpu-simulator:", err)
		os.Exit(1)
	}
}

func run() error {
	owner, err := simulator.OpenOwner(envOr("SIM_OWNER_DSN", ""))
	if err != nil {
		return fmt.Errorf("open owner db: %w", err)
	}
	defer func() { _ = owner.Close() }()
	provider, err := simulator.OpenProvider(envOr("SIM_PROVIDER_DSN", ""))
	if err != nil {
		return fmt.Errorf("open provider db: %w", err)
	}
	defer func() { _ = provider.Close() }()

	sim := simulator.NewSimulator(owner, provider)

	// 重启对账：按 operation/resource ID 查询 provider 补齐 owner 状态，不重复分配。
	ctx := context.Background()
	if err = sim.Recover(ctx); err != nil {
		return fmt.Errorf("recover: %w", err)
	}

	srv, err := simulator.NewGRPCServer(simulator.ServerConfig{
		Address:  envOr("SIM_LISTEN_ADDR", "127.0.0.1:7790"),
		CAFile:   envOr("SIM_TLS_CA_FILE", ""),
		CertFile: envOr("SIM_TLS_CERT_FILE", ""),
		KeyFile:  envOr("SIM_TLS_KEY_FILE", ""),
	}, sim)
	if err != nil {
		return err
	}
	if err = srv.Start(); err != nil {
		return err
	}

	notifier, err := simulator.NewNotifier(owner, simulator.NotifierConfig{
		GovernanceAddr: envOr("ANI_QUOTA_INTERNAL_ADDR", ""),
		CAFile:         envOr("ANI_QUOTA_CA_FILE", ""),
		CertFile:       envOr("ANI_QUOTA_CERT_FILE", ""),
		KeyFile:        envOr("ANI_QUOTA_KEY_FILE", ""),
		Interval:       500 * time.Millisecond,
	})
	if err != nil {
		return fmt.Errorf("init release notifier: %w", err)
	}
	notifier.Start()

	control, err := simulator.NewControlPlane(sim,
		envOr("SIM_CONTROL_ADDR", "127.0.0.1:7791"),
		envOr("SIM_CONTROL_TOKEN_FILE", ""))
	if err != nil {
		return fmt.Errorf("init control plane: %w", err)
	}
	if err = control.Start(); err != nil {
		return err
	}
	fmt.Printf("quota-gpu-simulator: grpc=%s control=%s\n", srv.Addr(), control.Addr())

	// 优雅退出。
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	control.Stop()
	notifier.Stop()
	srv.Stop()
	return nil
}
