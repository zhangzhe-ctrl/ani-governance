package data

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	quotasql "go-wind-admin/app/admin/service/internal/data/quotasql"
)

// quotaTransaction borrows the configured pgx connection for the entire Raw
// callback. No driver object or transaction escapes Raw; Ent never participates
// in this transaction. SQL is available only through generated query methods.
func quotaTransaction(ctx context.Context, db *sql.DB, fn func(*quotasql.Queries) error) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	return conn.Raw(func(driverConn any) error {
		// otelsql exposes its wrapped driver through Raw; keep that unwrap
		// strictly inside database/sql's connection lifetime callback.
		if wrapped, ok := driverConn.(interface{ Raw() driver.Conn }); ok {
			driverConn = wrapped.Raw()
		}
		pg, ok := driverConn.(*stdlib.Conn)
		if !ok {
			return fmt.Errorf("quota persistence requires the pgx PostgreSQL driver")
		}
		tx, err := pg.Conn().BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			return err
		}
		defer tx.Rollback(context.WithoutCancel(ctx))
		if err := fn(quotasql.New(tx)); err != nil {
			return err
		}
		return tx.Commit(ctx)
	})
}
