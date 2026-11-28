// Command aavanam-conform is the conformance driver of the backend. It connects the Python vector runner
// to the writer store over the line protocol of package conformance.
//
// The storage `pg` is the Go core on PostgreSQL, with the DSN of AAVANAM_PG_DSN. A reset removes each row
// of the database, and the command `restore` replaces its schema, so the DSN must name a database for tests only. The storage `mem` is the Go core on a
// storage in memory. The storage `null` refuses each call.
//
// With the storage `pg` or `mem`, the driver also has the network read binding of the writer store
// (document 22) on a port of the loopback address. Its first line gives the address as `read_url`.
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mohanraj00/aavanam/backend/api"
	"github.com/mohanraj00/aavanam/backend/conformance"
	"github.com/mohanraj00/aavanam/backend/core/memstore"
	"github.com/mohanraj00/aavanam/backend/pg"
)

// load replaces the content of the test database with the statements of a dump: the schema and the rows
// that an earlier release wrote (MIG-VER-1). It removes the schema `public` with each table first. The dump
// sets values of its session, so load runs it on a connection of its own.
func load(ctx context.Context, dsn, dump string) error {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return fmt.Errorf("PostgreSQL does not answer: %w", err)
	}
	defer conn.Close(context.WithoutCancel(ctx))
	// The simple protocol sends many statements in one message.
	if _, err := conn.Exec(ctx, `DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public`, pgx.QueryExecModeSimpleProtocol); err != nil {
		return fmt.Errorf("cannot remove the schema of the database: %w", err)
	}
	if _, err := conn.Exec(ctx, dump, pgx.QueryExecModeSimpleProtocol); err != nil {
		return fmt.Errorf("the dump did not load: %w", err)
	}
	return nil
}

func main() {
	fallback := os.Getenv("AAVANAM_STORAGE")
	if fallback == "" {
		fallback = "pg"
	}
	storage := flag.String("storage", fallback, "the storage: pg, mem or null (default: AAVANAM_STORAGE, then pg)")
	flag.Parse()
	debug := os.Getenv("AAVANAM_DEBUG") != ""
	var store conformance.Store
	var core *conformance.Core
	switch *storage {
	case "null":
		store = conformance.Null{}
	case "mem":
		core = &conformance.Core{Debug: debug, Open: func() (map[string]conformance.Backing, error) {
			device := memstore.New()
			return map[string]conformance.Backing{"writer": memstore.New(), "device": device, "output": device.Output()}, nil
		}}
	case "pg":
		dsn := os.Getenv("AAVANAM_PG_DSN")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		database, err := pg.Open(ctx, dsn)
		cancel()
		if err != nil {
			fmt.Fprintln(os.Stderr, "aavanam-conform:", err)
			os.Exit(2)
		}
		defer func() { database.Close() }()
		core = &conformance.Core{Debug: debug, Open: func() (map[string]conformance.Backing, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			return map[string]conformance.Backing{"writer": database}, database.Truncate(ctx)
		}}
		// The stored form of this storage is a dump of a database that an earlier release wrote. The open
		// after the load applies the schema changes that the dump does not have (MIG-VER-1, MIG-STG-1).
		core.Stored = func(path string) (map[string]conformance.Backing, error) {
			dump, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			database.Close() // the load removes each table, so no connection of the pool stays open
			err = load(ctx, dsn, string(dump))
			opened := database
			if err == nil {
				opened, err = pg.Open(ctx, dsn)
			}
			if err != nil {
				// The database has no schema, or a part of one. It gets an empty schema again, so that the
				// next reset finds its tables. Only the vector of this command fails.
				failure := load(ctx, dsn, `SELECT 1`)
				if failure == nil {
					opened, failure = pg.Open(ctx, dsn)
				}
				if failure != nil {
					fmt.Fprintln(os.Stderr, "aavanam-conform: the database does not open after a restore that failed:", failure)
					os.Exit(2)
				}
				database = opened
				return nil, err
			}
			database = opened
			return map[string]conformance.Backing{"writer": database}, nil
		}
	default:
		fmt.Fprintf(os.Stderr, "aavanam-conform: unknown storage %q\n", *storage)
		os.Exit(2)
	}
	if core != nil {
		store = core
	}
	if err := store.Reset(); err != nil {
		fmt.Fprintln(os.Stderr, "aavanam-conform:", err)
		os.Exit(2)
	}
	hello := conformance.Hello{Ready: true}
	if core != nil {
		// The loopback address and a port that the system gives (NET-TRN-5). The listener has only the
		// operations of the read binding.
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			fmt.Fprintln(os.Stderr, "aavanam-conform:", err)
			os.Exit(2)
		}
		server := api.Server(api.Read(api.Config{Store: core.Writer}), 0)
		go func() { _ = server.Serve(listener) }()
		defer server.Close()
		hello.ReadURL = "http://" + listener.Addr().String()
	}
	if err := conformance.Serve(os.Stdin, os.Stdout, store, hello); err != nil {
		fmt.Fprintln(os.Stderr, "aavanam-conform:", err)
		os.Exit(1)
	}
}
