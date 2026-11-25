// Command bench is the budget bench of the backend (docs/design/backend.md, section 7). It makes the
// synthetic store of a shape on each given storage, then it measures the reads over the network read
// binding on the loopback address, and the writes of a publication in the process.
//
// The PostgreSQL storage needs AAVANAM_PG_DSN. The bench removes each row of that database first, as the
// conformance driver does. Use a database for tests only.
//
//	go run ./bench -shape S0 -storage mem,pg -check -out result.json
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mohanraj00/aavanam/backend/api"
	"github.com/mohanraj00/aavanam/backend/bench/measure"
	"github.com/mohanraj00/aavanam/backend/bench/suite"
	"github.com/mohanraj00/aavanam/backend/bench/synth"
	"github.com/mohanraj00/aavanam/backend/conformance"
	"github.com/mohanraj00/aavanam/backend/core/memstore"
	"github.com/mohanraj00/aavanam/backend/core/ops"
	"github.com/mohanraj00/aavanam/backend/pg"
)

type options struct {
	shape     synth.Shape
	storages  []string
	calls     int
	clients   int
	publishes int
	seed      uint64
	out       string
	check     bool
	factor    float64
	limit     time.Duration
	known     map[string]string
}

func parse(args []string) (options, error) {
	var o options
	flags := flag.NewFlagSet("bench", flag.ContinueOnError)
	shape := flags.String("shape", "S0", "the store shape: S0, S1 or tiny")
	storages := flags.String("storage", "mem", "a comma-separated list of storages: mem, pg")
	flags.IntVar(&o.calls, "calls", 1000, "the calls of each read measure")
	flags.IntVar(&o.clients, "clients", 0, "if more than 1, also run each read measure with this number of concurrent clients")
	flags.IntVar(&o.publishes, "publishes", 5, "the publications of 1 000 new descriptors")
	flags.Uint64Var(&o.seed, "seed", 1, "the seed of the synthetic store")
	flags.StringVar(&o.out, "out", "", "the path of the JSON result file")
	flags.BoolVar(&o.check, "check", false, "fail if a p95 is more than -factor times its target")
	flags.Float64Var(&o.factor, "factor", 3, "the factor of a clear miss of a target")
	flags.DurationVar(&o.limit, "max-time", 0, "if more than 0, the time limit of each read measure (each measure still makes 20 calls or more)")
	known := flags.String("known", "", "the known misses that -check does not fail: storage/measure=issue, separated by commas")
	if err := flags.Parse(args); err != nil {
		return o, err
	}
	var err error
	if o.known, err = measure.ParseKnown(*known); err != nil {
		return o, err
	}
	var ok bool
	if o.shape, ok = synth.ShapeOf(*shape); !ok {
		return o, fmt.Errorf("unknown shape %q", *shape)
	}
	for _, s := range strings.Split(*storages, ",") {
		if s = strings.TrimSpace(s); s != "mem" && s != "pg" {
			return o, fmt.Errorf("unknown storage %q", s)
		}
		o.storages = append(o.storages, strings.TrimSpace(s))
	}
	if o.calls < 1 {
		return o, errors.New("-calls must be 1 or more")
	}
	return o, nil
}

// backing is one storage of a run, and the values of the storage after the build.
type backing struct {
	storage ops.Storage
	after   func(ctx context.Context, run *measure.StorageRun) error
	close   func()
}

func open(ctx context.Context, name string, machine *measure.Machine) (backing, error) {
	if name == "mem" {
		return backing{storage: memstore.New(), after: func(context.Context, *measure.StorageRun) error { return nil }, close: func() {}}, nil
	}
	dsn := os.Getenv("AAVANAM_PG_DSN")
	store, err := pg.Open(ctx, dsn)
	if err != nil {
		return backing{}, err
	}
	if err := store.Truncate(ctx); err != nil {
		store.Close()
		return backing{}, err
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		store.Close()
		return backing{}, err
	}
	if err := conn.QueryRow(ctx, `SHOW server_version`).Scan(&machine.PostgreSQL); err != nil {
		return backing{}, err
	}
	after := func(ctx context.Context, run *measure.StorageRun) error {
		// The statistics of the planner after a bulk load. Autovacuum gives them later, so the bench makes
		// them at a fixed point.
		if _, err := conn.Exec(ctx, `ANALYZE`); err != nil {
			return err
		}
		var size int64
		err := conn.QueryRow(ctx, `SELECT coalesce(sum(pg_total_relation_size(c.oid)), 0)::bigint FROM pg_class c
			JOIN pg_namespace n ON n.oid = c.relnamespace WHERE c.relkind = 'r' AND n.nspname = current_schema()`).Scan(&size)
		run.Values["pg_relations_bytes"] = size
		return err
	}
	return backing{storage: store, after: after, close: func() { conn.Close(context.Background()); store.Close() }}, nil
}

// serve starts the network read binding of a store on the loopback address and returns its caller.
func serve(store *ops.Store) (suite.Caller, func(), error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, nil, err
	}
	server := api.Server(api.Read(api.Config{Store: func() *ops.Store { return store }}), 0)
	go server.Serve(listener)
	base := "http://" + listener.Addr().String() + "/v1/op/"
	client := &http.Client{Transport: &http.Transport{MaxIdleConnsPerHost: 128}}
	call := func(operation string, request []byte) ([]byte, error) {
		response, err := client.Post(base+operation, "application/json", bytes.NewReader(request))
		if err != nil {
			return nil, err
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err == nil && response.StatusCode != http.StatusOK {
			err = fmt.Errorf("HTTP status %d", response.StatusCode)
		}
		return body, err
	}
	return call, func() { server.Close() }, nil
}

func runOne(ctx context.Context, o options, name string, machine *measure.Machine) (measure.StorageRun, error) {
	run := measure.StorageRun{Storage: name, Values: map[string]int64{}, Times: map[string]float64{}}
	b, err := open(ctx, name, machine)
	if err != nil {
		return run, err
	}
	defer b.close()
	store := ops.New(ops.Writer, b.storage, conformance.Registry())
	start := time.Now()
	built, err := synth.Build(ctx, store, o.shape, o.seed)
	if err != nil {
		return run, fmt.Errorf("build: %w", err)
	}
	run.Times["build"] = measure.Ms(time.Since(start))
	run.Store = built.Summary() // before the publications of the bench add to the store
	if err := b.after(ctx, &run); err != nil {
		return run, err
	}
	if name == "pg" {
		// The size target of section 7: 1.5 times the payload bytes plus 2 KiB for each descriptor.
		run.Values["pg_relations_target_bytes"] = built.PayloadBytes*3/2 + int64(built.Descriptors)*2048
	}
	call, stop, err := serve(store)
	if err != nil {
		return run, err
	}
	defer stop()
	spec := suite.Spec{Shape: o.shape.Name, Open: map[string]any{"heads": []string{built.Main()}}, Store: built,
		Calls: o.calls, Clients: 1, Matches: built.Matches, Limit: o.limit}
	measures, err := suite.Reads(call, spec)
	run.Measures = append(run.Measures, measures...)
	if err != nil {
		return run, err
	}
	if o.clients > 1 {
		spec.Clients = o.clients
		measures, err := suite.Reads(call, spec)
		run.Measures = append(run.Measures, measures...)
		if err != nil {
			return run, err
		}
	}
	var steps [4][]time.Duration
	descriptors := 0
	for range o.publishes {
		t, err := synth.AddUnits(ctx, store, built)
		if err != nil {
			return run, fmt.Errorf("publish: %w", err)
		}
		descriptors = t.Descriptors
		for i, d := range []time.Duration{t.Put, t.Snapshot, t.Candidate, t.Publish} {
			steps[i] = append(steps[i], d)
		}
	}
	if o.publishes > 0 {
		for i, step := range []string{"put", "create_snapshot", "create_candidate", "publish"} {
			run.Measures = append(run.Measures, measure.Of(fmt.Sprintf("publish_%d:%s", descriptors, step), 1, steps[i]))
		}
	}
	return run, nil
}

func run(args []string) error {
	o, err := parse(args)
	if err != nil {
		return err
	}
	ctx := context.Background()
	machine := measure.Describe()
	report := measure.NewReport(machine, o.shape.Name, o.calls, o.factor)
	report.Limit, report.Known = o.limit.Seconds(), o.known
	for _, name := range o.storages {
		fmt.Fprintf(os.Stderr, "bench: %s on %s\n", o.shape.Name, name)
		r, err := runOne(ctx, o, name, &machine)
		report.Runs = append(report.Runs, r)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	report.Machine = machine
	report.MarkKnown(o.known)
	report.Print(os.Stdout)
	if o.out != "" {
		if err := report.Write(o.out); err != nil {
			return err
		}
	}
	if misses := report.Misses(); o.check && len(misses) > 0 {
		return errors.New("clear misses of the budget:\n  " + strings.Join(misses, "\n  "))
	}
	return nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "bench:", err)
		os.Exit(1)
	}
}
