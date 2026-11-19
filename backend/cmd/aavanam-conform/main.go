// Command aavanam-conform is the conformance driver of the backend. It connects the Python vector runner
// to the writer store over the line protocol of package conformance.
//
// The storage `pg` is the Go core on PostgreSQL, with the DSN of AAVANAM_PG_DSN. A reset removes each row
// of the database, so the DSN must name a database for tests only. The storage `mem` is the Go core on a
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

	"github.com/mohanraj00/aavanam/backend/api"
	"github.com/mohanraj00/aavanam/backend/conformance"
	"github.com/mohanraj00/aavanam/backend/core/memstore"
	"github.com/mohanraj00/aavanam/backend/pg"
)

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
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		database, err := pg.Open(ctx, os.Getenv("AAVANAM_PG_DSN"))
		cancel()
		if err != nil {
			fmt.Fprintln(os.Stderr, "aavanam-conform:", err)
			os.Exit(2)
		}
		defer database.Close()
		core = &conformance.Core{Debug: debug, Open: func() (map[string]conformance.Backing, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			return map[string]conformance.Backing{"writer": database}, database.Truncate(ctx)
		}}
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
