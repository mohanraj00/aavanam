// Command bench is the budget bench of the device store (docs/design/backend.md, sections 7 and 8). It
// makes the synthetic store of a shape in a writer store in memory, exports the heads as a device bundle,
// and imports the bundle into a new device store. It measures the bundle, the import, a bundle update that
// changes 1% of the artifacts, the device file sizes and the device reads. The reads call the device store
// in the process, as an application on a device does.
//
//	go run ./bench -shape S0 -check -out result.json
package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/mohanraj00/aavanam/backend/bench/measure"
	"github.com/mohanraj00/aavanam/backend/bench/suite"
	"github.com/mohanraj00/aavanam/backend/bench/synth"
	"github.com/mohanraj00/aavanam/backend/conformance"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/memstore"
	"github.com/mohanraj00/aavanam/backend/core/ops"
	"github.com/mohanraj00/aavanam/reference/device"
)

type options struct {
	shape  synth.Shape
	calls  int
	seed   uint64
	out    string
	dir    string
	check  bool
	factor float64
	limit  time.Duration
	known  map[string]string
}

func parse(args []string) (options, error) {
	var o options
	flags := flag.NewFlagSet("bench", flag.ContinueOnError)
	shape := flags.String("shape", "S0", "the store shape: S0, S1 or tiny")
	flags.IntVar(&o.calls, "calls", 1000, "the calls of each read measure")
	flags.Uint64Var(&o.seed, "seed", 1, "the seed of the synthetic store")
	flags.StringVar(&o.out, "out", "", "the path of the JSON result file")
	flags.StringVar(&o.dir, "dir", "", "an empty directory for the device files (default: a new temporary directory, removed at the end)")
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
	if o.calls < 1 {
		return o, errors.New("-calls must be 1 or more")
	}
	return o, nil
}

func caller(ctx context.Context, store *ops.Store) suite.Caller {
	return func(operation string, request []byte) ([]byte, error) {
		return store.Call(ctx, jsontext.S(operation), request).JSON(), nil
	}
}

// bundle is an exported bundle: its JSON text, its roots and the bytes of its payloads.
type bundle struct {
	text         json.RawMessage
	roots        []string
	payloadBytes int64
}

func export(call suite.Caller, request map[string]any) (bundle, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return bundle{}, err
	}
	out, err := call("export", body)
	if err != nil {
		return bundle{}, err
	}
	var result struct {
		Status string          `json:"status"`
		Bundle json.RawMessage `json:"bundle"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return bundle{}, err
	}
	if result.Status != "ok" {
		_, err := suite.Decode(out)
		return bundle{}, fmt.Errorf("export: %w", err)
	}
	var content struct {
		Roots    []string          `json:"roots"`
		Payloads map[string]string `json:"payloads"`
	}
	if err := json.Unmarshal(result.Bundle, &content); err != nil {
		return bundle{}, err
	}
	b := bundle{text: result.Bundle, roots: content.Roots}
	for _, data := range content.Payloads {
		n, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			return bundle{}, err
		}
		b.payloadBytes += int64(len(n))
	}
	return b, nil
}

// deviceStore is one device store and its local output store in a directory.
type deviceStore struct {
	dir      string
	registry *ops.Registry
	store    *ops.Store
	stores   []*device.Store
}

func newDevice(dir string, registry *ops.Registry) (*deviceStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if err := device.Create(dir); err != nil {
		return nil, err
	}
	dev, out, err := device.Open(dir)
	if err != nil {
		return nil, err
	}
	store := ops.New(ops.Device, dev, registry)
	store.Output = ops.New(ops.Output, out, registry)
	return &deviceStore{dir: dir, registry: registry, store: store, stores: []*device.Store{dev, out}}, nil
}

// close closes the output store first. It attaches the device file, and the last connection to the
// device file moves the write-ahead log into the file.
func (d *deviceStore) close() {
	for i := len(d.stores) - 1; i >= 0; i-- {
		d.stores[i].Close()
	}
}

// size returns the bytes of a file, or 0 if it is not there.
func size(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

// sizes closes the store, so that SQLite moves the write-ahead log into the device file, and records the
// bytes of the files. Then it opens the store again.
func (d *deviceStore) sizes(values map[string]int64, prefix string) error {
	d.close()
	file := filepath.Join(d.dir, device.DeviceFile)
	values[prefix+"device_file_bytes"] = size(file)
	values[prefix+"device_wal_bytes"] = size(file + "-wal")
	dev, out, err := device.Open(d.dir)
	if err != nil {
		return err
	}
	registry := d.registry
	d.store = ops.New(ops.Device, dev, registry)
	d.store.Output = ops.New(ops.Output, out, registry)
	d.stores = []*device.Store{dev, out}
	return nil
}

// load imports a bundle and returns the time of the import.
func (d *deviceStore) load(ctx context.Context, b bundle) (time.Duration, error) {
	request := append(append([]byte(`{"bundle":`), b.text...), '}')
	start := time.Now()
	out := d.store.Call(ctx, jsontext.S("import"), request).JSON()
	elapsed := time.Since(start)
	_, err := suite.Decode(out)
	return elapsed, err
}

func sqliteVersion() string {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return ""
	}
	defer db.Close()
	var v string
	if db.QueryRow(`SELECT sqlite_version()`).Scan(&v) != nil {
		return ""
	}
	return v
}

func runDevice(ctx context.Context, o options, dir string) (measure.StorageRun, error) {
	run := measure.StorageRun{Storage: "device", Values: map[string]int64{}, Times: map[string]float64{}}
	registry := conformance.Registry()
	writer := ops.New(ops.Writer, memstore.New(), registry)
	start := time.Now()
	built, err := synth.Build(ctx, writer, o.shape, o.seed)
	if err != nil {
		return run, fmt.Errorf("build: %w", err)
	}
	run.Times["writer_build"] = measure.Ms(time.Since(start))
	run.Store = built.Summary()
	write := caller(ctx, writer)

	// The first bundle: the head of each collection.
	opened, err := suite.Do(write, "open_context", map[string]any{"heads": built.Collections})
	if err != nil {
		return run, err
	}
	start = time.Now()
	first, err := export(write, map[string]any{"context": opened.Context, "roots": built.Heads})
	if err != nil {
		return run, err
	}
	run.Times["export"] = measure.Ms(time.Since(start))
	run.Values["bundle_bytes"] = int64(len(first.text))
	run.Values["bundle_payload_bytes"] = first.payloadBytes
	run.Values["bundle_ratio_permille"] = int64(len(first.text)) * 1000 / first.payloadBytes

	dev, err := newDevice(filepath.Join(dir, "full"), registry)
	if err != nil {
		return run, err
	}
	defer dev.close()
	elapsed, err := dev.load(ctx, first)
	if err != nil {
		return run, fmt.Errorf("import: %w", err)
	}
	run.Times["first_import"] = measure.Ms(elapsed)
	if err := dev.sizes(run.Values, "after_import:"); err != nil {
		return run, err
	}

	// The reads of the device, on the root of the main collection of the first bundle.
	spec := suite.Spec{Shape: o.shape.Name, Open: map[string]any{"roots": []string{built.Heads[0]}}, Store: built,
		Calls: o.calls, Clients: 1, Matches: built.Matches, Limit: o.limit}
	measures, err := suite.Reads(caller(ctx, dev.store), spec)
	run.Measures = append(run.Measures, measures...)
	if err != nil {
		return run, err
	}

	// The bundle update: 1% of the artifacts change. An export is the full closure of its roots (BUN-EXP-2).
	added, err := synth.Update(ctx, writer, built)
	if err != nil {
		return run, fmt.Errorf("update: %w", err)
	}
	run.Values["update_new_descriptors"] = int64(added)
	opened, err = suite.Do(write, "open_context", map[string]any{"heads": built.Collections})
	if err != nil {
		return run, err
	}
	second, err := export(write, map[string]any{"context": opened.Context, "roots": built.Heads})
	if err != nil {
		return run, err
	}
	run.Values["update_bundle_bytes"] = int64(len(second.text))
	elapsed, err = dev.load(ctx, second)
	if err != nil {
		return run, fmt.Errorf("update import: %w", err)
	}
	run.Times["update_import"] = measure.Ms(elapsed)
	if err := dev.sizes(run.Values, "after_update:"); err != nil {
		return run, err
	}

	// A filtered bundle of the main head: its roots are the matched knowledge units (BUN-FLT-3).
	opened, err = suite.Do(write, "open_context", map[string]any{"heads": []string{built.Main()}})
	if err != nil {
		return run, err
	}
	filtered, err := export(write, map[string]any{"context": opened.Context, "roots": []string{built.Heads[0]},
		"filter": map[string]any{"equals": map[string]any{"region": synth.Region}}})
	if err != nil {
		return run, err
	}
	run.Values["filtered_bundle_roots"] = int64(len(filtered.roots))
	run.Values["filtered_bundle_bytes"] = int64(len(filtered.text))
	small, err := newDevice(filepath.Join(dir, "filtered"), registry)
	if err != nil {
		return run, err
	}
	defer small.close()
	elapsed, err = small.load(ctx, filtered)
	if err != nil {
		return run, fmt.Errorf("filtered import: %w", err)
	}
	run.Times["filtered_import"] = measure.Ms(elapsed)
	read := caller(ctx, small.store)
	openRoots, _ := json.Marshal(map[string]any{"roots": filtered.roots})
	result, err := measure.Run("filtered:open_context", o.calls, o.limit,
		func(int) ([]byte, error) { return read("open_context", openRoots) }, func(b []byte) error { _, err := suite.Decode(b); return err })
	if err != nil {
		return run, err
	}
	run.Measures = append(run.Measures, result)
	ctxFiltered, err := suite.Do(read, "open_context", map[string]any{"roots": filtered.roots})
	if err != nil {
		return run, err
	}
	search, _ := json.Marshal(map[string]any{"context": ctxFiltered.Context, "query": synth.Query})
	result, err = measure.Run("filtered:search_text", o.calls, o.limit,
		func(int) ([]byte, error) { return read("search_text", search) }, func(b []byte) error { _, err := suite.Decode(b); return err })
	run.Measures = append(run.Measures, result)
	return run, err
}

func run(args []string) error {
	o, err := parse(args)
	if err != nil {
		return err
	}
	dir := o.dir
	if dir == "" {
		if dir, err = os.MkdirTemp("", "aavanam-bench-"); err != nil {
			return err
		}
		defer os.RemoveAll(dir)
	}
	machine := measure.Describe()
	machine.SQLite = sqliteVersion()
	report := measure.NewReport(machine, o.shape.Name, o.calls, o.factor)
	report.Limit, report.Known = o.limit.Seconds(), o.known
	fmt.Fprintf(os.Stderr, "bench: %s on device\n", o.shape.Name)
	r, err := runDevice(context.Background(), o, dir)
	report.Runs = append(report.Runs, r)
	if err != nil {
		return err
	}
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
