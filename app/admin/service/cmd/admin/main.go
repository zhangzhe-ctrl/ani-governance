// admin performs explicit deployment and management operations without starting
// HTTP or changing the schema. ModelDev management also verifies Redis sessions.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go-wind-admin/app/admin/service/cmd/server/assets"
	dbbootstrap "go-wind-admin/sql/bootstrap"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	return runAdmin(context.Background(), os.Args[1:], os.Stdout)
}

func runAdmin(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: admin <init|check|sync-apis|modeldev-pause> [--username admin] [--password-file PATH] [--dry-run]; connection: ANI_DATABASE_DSN")
	}
	command := args[0]
	if command == "modeldev-pause" {
		return runModelDevPause(ctx, args[1:], stdout)
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	username := flags.String("username", "admin", "first platform administrator username (init only)")
	passwordFile := flags.String("password-file", "", "file containing the first administrator password (init only)")
	dryRun := flags.Bool("dry-run", false, "show API changes and roll back (sync-apis only)")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments")
	}
	if command != "init" && command != "check" && command != "sync-apis" {
		return fmt.Errorf("unknown command %q", command)
	}
	if *dryRun && command != "sync-apis" {
		return fmt.Errorf("--dry-run applies to sync-apis only")
	}
	var invalidFlag string
	flags.Visit(func(f *flag.Flag) {
		if command != "init" && (f.Name == "username" || f.Name == "password-file") {
			invalidFlag = f.Name
		}
	})
	if invalidFlag != "" {
		return fmt.Errorf("--%s applies to init only", invalidFlag)
	}
	dsn := os.Getenv("ANI_DATABASE_DSN")
	if dsn == "" {
		return fmt.Errorf("ANI_DATABASE_DSN is required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("invalid database connection configuration")
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		return fmt.Errorf("database connection failed; check ANI_DATABASE_DSN and PostgreSQL availability")
	}
	if command == "check" {
		if err = dbbootstrap.Check(ctx, db); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "PASS: platform administrator and required API grants are present")
		return nil
	}
	catalog, err := dbbootstrap.Catalog(assets.OpenApiData)
	if err != nil {
		return err
	}
	if command == "init" {
		var password string
		if *passwordFile != "" {
			b, e := os.ReadFile(*passwordFile)
			if e != nil {
				return e
			}
			password = strings.TrimSuffix(strings.TrimSuffix(string(b), "\n"), "\r")
		}
		created, e := dbbootstrap.Initialize(ctx, db, catalog, *username, password)
		if e != nil {
			return e
		}
		if e = dbbootstrap.Check(ctx, db); e != nil {
			return e
		}
		if created {
			fmt.Fprintln(stdout, "PASS: initial seed and administrator committed")
		} else {
			fmt.Fprintln(stdout, "UNCHANGED: initialization already completed; existing data preserved")
		}
		return nil
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: *dryRun})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	changes, err := dbbootstrap.SyncAPIs(ctx, tx, catalog, *dryRun)
	if err != nil {
		return err
	}
	for _, change := range changes {
		fmt.Fprintln(stdout, change)
	}
	if *dryRun {
		fmt.Fprintln(stdout, "DRY RUN: no changes written")
		return nil
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "PASS: API catalog synchronized; permissions unchanged. Reload running instances to refresh authorization policies.")
	return nil
}
