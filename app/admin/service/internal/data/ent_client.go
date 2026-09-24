package data

import (
	"context"
	"fmt"

	"entgo.io/ent/dialect/sql"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"

	entCrud "go-wind-admin/pkg/localdeps/go-crud/entgo"

	"github.com/tx7do/kratos-bootstrap/bootstrap"
	entBootstrap "go-wind-admin/pkg/localdeps/kratos-bootstrap/database/ent"

	"go-wind-admin/app/admin/service/internal/data/ent"
	_ "go-wind-admin/app/admin/service/internal/data/ent/runtime"
)

// NewEntClient 创建Ent ORM数据库客户端
func NewEntClient(ctx *bootstrap.Context) (*entCrud.EntClient[*ent.Client], func(), error) {
	l := ctx.NewLoggerHelper("ent/data/admin-service")

	cfg := ctx.GetConfig()
	if cfg == nil || cfg.Data == nil || cfg.Data.Database == nil {
		l.Errorf(context.Background(), "[ENT] failed getting config")
		return nil, nil, fmt.Errorf("[ENT] failed getting config")
	}

	if cfg.Data.Database.GetMigrate() {
		return nil, nil, fmt.Errorf("data.database.migrate=true is no longer supported: apply schema migrations with Atlas before starting ani-governance")
	}

	cli, err := entBootstrap.NewEntClient(cfg, func(drv *sql.Driver) *ent.Client {
		client := ent.NewClient(
			ent.Driver(&auditDriver{drv}),
			ent.Log(func(a ...any) {
				l.Debug(context.Background(), fmt.Sprint(a...))
			}),
		)
		if client == nil {
			l.Errorf(context.Background(), "[ENT] failed creating ent client")
			panic("[ENT] failed creating ent client")
		}

		return client
	})
	if err != nil {
		l.Errorf(context.Background(), "[ENT] failed creating ent client: %v", err)
		return nil, nil, fmt.Errorf("[ENT] failed creating ent client: %w", err)
	}

	return cli, func() {
		if cleanErr := cli.Close(); cleanErr != nil {
			l.Errorf(context.Background(), "[ENT] failed closing ent client: %v", cleanErr)
		}
	}, nil
}
