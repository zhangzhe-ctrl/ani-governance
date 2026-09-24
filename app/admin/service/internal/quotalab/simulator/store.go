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

// OpenOwner opens an explicitly migrated owner database without DDL.
func OpenOwner(dsn string) (*OwnerStore, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	if err = db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("init owner db: %w", err)
	}
	return &OwnerStore{db: db}, nil
}

// OpenProvider opens an explicitly migrated provider database without DDL.
func OpenProvider(dsn string) (*ProviderStore, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	if err = db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("init provider db: %w", err)
	}
	return &ProviderStore{db: db}, nil
}

// Close 关闭连接。
func (s *OwnerStore) Close() error    { return s.db.Close() }
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
