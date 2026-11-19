// Command aavanam-conform is the conformance driver of the backend. It connects the Python vector runner
// to the writer store over the line protocol of package conformance.
//
// In this version each storage gives `unsupported_operation` for each call. The storage `pg` also checks
// the connection to PostgreSQL. A later change adds the stores.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mohanraj00/aavanam/backend/conformance"
)

func main() {
	fallback := os.Getenv("AAVANAM_STORAGE")
	if fallback == "" {
		fallback = "pg"
	}
	storage := flag.String("storage", fallback, "the storage: pg, mem or null (default: AAVANAM_STORAGE, then pg)")
	flag.Parse()
	var store conformance.Store
	switch *storage {
	case "null", "mem":
		store = conformance.Null{}
	case "pg":
		pool, err := connect(os.Getenv("AAVANAM_PG_DSN"))
		if err != nil {
			fmt.Fprintln(os.Stderr, "aavanam-conform:", err)
			os.Exit(2)
		}
		defer pool.Close()
		store = conformance.Null{}
	default:
		fmt.Fprintf(os.Stderr, "aavanam-conform: unknown storage %q\n", *storage)
		os.Exit(2)
	}
	if err := conformance.Serve(os.Stdin, os.Stdout, store, conformance.Hello{Ready: true}); err != nil {
		fmt.Fprintln(os.Stderr, "aavanam-conform:", err)
		os.Exit(1)
	}
}

// connect opens a pool and checks that PostgreSQL answers. It does not print the DSN, because a DSN can
// have a password.
func connect(dsn string) (*pgxpool.Pool, error) {
	if dsn == "" {
		return nil, fmt.Errorf("the storage pg needs AAVANAM_PG_DSN")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("the PostgreSQL configuration is not valid")
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("PostgreSQL does not answer: %w", err)
	}
	return pool, nil
}
