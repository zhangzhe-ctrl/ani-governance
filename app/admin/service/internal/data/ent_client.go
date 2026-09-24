package data

import (
	"context"
	"fmt"

	"entgo.io/ent/dialect/sql"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/lib/pq"

	entCrud "github.com/tx7do/go-crud/entgo"

	conf "github.com/tx7do/kratos-bootstrap/api/gen/go/conf/v1"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	entBootstrap "github.com/tx7do/kratos-bootstrap/database/ent"
	"google.golang.org/protobuf/proto"

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

	// PostgreSQL keeps Ent's postgres dialect while sharing the pgx stdlib
	// pool with the quota transaction adapter. Do not silently select lib/pq
	// through the legacy "postgres" registration or mutate the caller's config.
	postgres := cfg.Data.Database.GetDriver() == "postgres" || cfg.Data.Database.GetDriver() == "pgx"
	driverCfg := cfg
	if postgres {
		driverCfg = proto.Clone(cfg).(*conf.Bootstrap)
		driverCfg.Data.Database.Driver = "pgx"
	}
	cli, err := entBootstrap.NewEntClient(driverCfg, func(drv *sql.Driver) *ent.Client {
		if postgres {
			drv = sql.OpenDB("postgres", drv.DB())
		}
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
	if postgres {
		cli = entCrud.NewEntClient(cli.Client(), sql.OpenDB("postgres", cli.DB()))
	}

	return cli, func() {
		if cleanErr := cli.Close(); cleanErr != nil {
			l.Errorf(context.Background(), "[ENT] failed closing ent client: %v", cleanErr)
		}
	}, nil
}
