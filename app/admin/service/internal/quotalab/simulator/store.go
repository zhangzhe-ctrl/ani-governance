//go:build quota_lab

package simulator

// GPU 模拟资源服务：独立进程、独立持久化（计划 §11.2）。
// owner 与 provider 使用不同数据库和连接，不存在跨库事务。
// provider 只是硬件行为的持久化模拟，不是物理 GPU 分配证据。

import (
	"context"
	"database/sql"
	"fmt"
)

// OwnerStore 持久化命令/幂等、资源记录、累计释放事实、待发送退额通知。
type OwnerStore struct {
	db *sql.DB
}

// ProviderStore 持久化 GPU 单元分配与 operation 执行代次/封闭标记。
type ProviderStore struct {
	db *sql.DB
}

// OpenOwner 打开（必要时初始化）owner 数据库。
func OpenOwner(dsn string) (*OwnerStore, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	if err = pingAndInit(db, ownerSchema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("init owner db: %w", err)
	}
	return &OwnerStore{db: db}, nil
}

// OpenProvider 打开（必要时初始化）provider 数据库。
func OpenProvider(dsn string) (*ProviderStore, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	if err = pingAndInit(db, providerSchema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("init provider db: %w", err)
	}
	return &ProviderStore{db: db}, nil
}

func pingAndInit(db *sql.DB, schema []string) error {
	if err := db.Ping(); err != nil {
		return err
	}
	for _, stmt := range schema {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("exec %q: %w", firstLine(stmt), err)
		}
	}
	return nil
}

func firstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}

// owner 数据库表（每次运行只创建缺失表；不清理既有事实）。
var ownerSchema = []string{
	`CREATE TABLE IF NOT EXISTS sim_commands (
		operation_id      text PRIMARY KEY,
		tenant_id         uuid NOT NULL,
		resource_id       text NOT NULL,
		create_operation_id text,
		kind              text NOT NULL,           -- create / delete
		status            text NOT NULL,           -- accepted(持久接受) / completed / aborted
		actor             text NOT NULL,
		request_hash      text NOT NULL,
		name              text,
		gpu_count         int,
		charge_id         text NOT NULL,
		created_at        timestamptz NOT NULL DEFAULT now(),
		updated_at        timestamptz NOT NULL DEFAULT now()
	)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS uix_sim_commands_create_resource
		ON sim_commands (tenant_id, resource_id) WHERE kind = 'create'`,
	`CREATE TABLE IF NOT EXISTS sim_units (
		charge_id     text NOT NULL,
		unit_ordinal  int  NOT NULL,
		tenant_id     uuid NOT NULL,
		resource_id   text NOT NULL,
		state         text NOT NULL,               -- allocated / released / aborted
		updated_at    timestamptz NOT NULL DEFAULT now(),
		PRIMARY KEY (charge_id, unit_ordinal)
	)`,
	`CREATE TABLE IF NOT EXISTS sim_release_facts (
		charge_id     text NOT NULL,
		unit_ordinal  int  NOT NULL,
		tenant_id     uuid NOT NULL,
		released_at   timestamptz NOT NULL DEFAULT now(),
		PRIMARY KEY (charge_id, unit_ordinal)
	)`,
	`CREATE TABLE IF NOT EXISTS sim_notify_queue (
		event_id     text PRIMARY KEY,
		charge_id    text NOT NULL,
		operation_id text NOT NULL,
		tenant_id    uuid NOT NULL,
		payload_hash text NOT NULL,
		payload_json text NOT NULL,
		reason       text NOT NULL,
		state        text NOT NULL DEFAULT 'pending', -- pending / delivered
		attempt      int NOT NULL DEFAULT 0,
		created_at   timestamptz NOT NULL DEFAULT now(),
		updated_at   timestamptz NOT NULL DEFAULT now()
	)`,
	`CREATE TABLE IF NOT EXISTS sim_control_log (
		id         bigserial PRIMARY KEY,
		action     text NOT NULL,
		detail     text,
		created_at timestamptz NOT NULL DEFAULT now()
	)`,
}

// provider 数据库表。
var providerSchema = []string{
	`CREATE TABLE IF NOT EXISTS sim_provider_ops (
		operation_id         text PRIMARY KEY,
		tenant_id            uuid NOT NULL,
		resource_id          text NOT NULL,
		execution_generation bigint NOT NULL DEFAULT 0,
		closed               boolean NOT NULL DEFAULT false,
		created_at           timestamptz NOT NULL DEFAULT now()
	)`,
	`CREATE TABLE IF NOT EXISTS sim_allocations (
		resource_id  text NOT NULL,
		ordinal      int  NOT NULL,
		tenant_id    uuid NOT NULL,
		operation_id text NOT NULL,
		state        text NOT NULL DEFAULT 'allocated', -- allocated / freed
		updated_at   timestamptz NOT NULL DEFAULT now(),
		PRIMARY KEY (resource_id, ordinal)
	)`,
	`CREATE TABLE IF NOT EXISTS sim_provider_capacity (
		id         int PRIMARY KEY DEFAULT 1,
		total      int NOT NULL,
		check (id = 1)
	)`,
	`INSERT INTO sim_provider_capacity (id, total) VALUES (1, 64) ON CONFLICT (id) DO NOTHING`,
}

// Close 关闭连接。
func (s *OwnerStore) Close() error   { return s.db.Close() }
func (s *ProviderStore) Close() error { return s.db.Close() }

// withOwnerTx 在 owner 单库事务中执行 fn。
func (s *OwnerStore) withOwnerTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err = fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
