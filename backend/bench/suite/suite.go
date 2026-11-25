// Package suite has the read measures of the budget bench. A caller sends one request and returns the
// result envelope. The backend bench calls over the network read binding. The device bench calls the
// device store in the process, as an application on a device does.
package suite

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/mohanraj00/aavanam/backend/bench/measure"
	"github.com/mohanraj00/aavanam/backend/bench/synth"
)

// Caller sends one request of an operation and returns the result envelope.
type Caller func(operation string, request []byte) ([]byte, error)

// Envelope is the part of a result envelope that the checks read.
type Envelope struct {
	Status  string           `json:"status"`
	Reasons []map[string]any `json:"reasons"`
	Context string           `json:"context"`
	Items   []map[string]any `json:"items"`
}

// Decode returns the envelope of a body. A status other than `ok` gives a measure.StatusError with the
// first reason code.
func Decode(body []byte) (Envelope, error) {
	var e Envelope
	if err := json.Unmarshal(body, &e); err != nil {
		return e, err
	}
	if e.Status != "ok" {
		code := ""
		if len(e.Reasons) > 0 {
			code, _ = e.Reasons[0]["code"].(string)
		}
		return e, &measure.StatusError{Status: e.Status, Code: code}
	}
	return e, nil
}

// Do sends one request and returns its `ok` envelope.
func Do(call Caller, operation string, request any) (Envelope, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return Envelope{}, err
	}
	out, err := call(operation, body)
	if err != nil {
		return Envelope{}, err
	}
	return Decode(out)
}

// Spec is one run of the read measures.
type Spec struct {
	Shape   string         // the shape name, for the targets
	Open    map[string]any // the request of `open_context`: its `heads` or its `roots`
	Store   *synth.Store
	Calls   int           // the calls of each measure
	Clients int           // the clients of each measure; 1 gives the targets of the design
	Matches int           // the items of the search, or -1 for no count check
	Limit   time.Duration // the time limit of each measure, or 0
	Prefix  string
}

func marshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// count returns a check of the number of items of one envelope, or of the rows of its first item.
func count(want int, rows bool) measure.Check {
	return func(body []byte) error {
		e, err := Decode(body)
		if err != nil || want < 0 {
			return err
		}
		got := len(e.Items)
		if rows && got > 0 {
			list, _ := e.Items[0]["rows"].([]any)
			got = len(list)
		}
		if got != want {
			return fmt.Errorf("%d items or rows, not %d", got, want)
		}
		return nil
	}
}

func ok(body []byte) error {
	_, err := Decode(body)
	return err
}

// Reads runs the read measures: `open_context`, a row read, a 64 KiB artifact read, `search_text` with
// two words, and `query_data` with one `where` filter on the largest table.
func Reads(call Caller, spec Spec) ([]measure.Result, error) {
	opened, err := Do(call, "open_context", spec.Open)
	if err != nil {
		return nil, fmt.Errorf("open_context: %w", err)
	}
	ctx := opened.Context
	s := spec.Store
	open := marshal(spec.Open)
	search := marshal(map[string]any{"context": ctx, "query": synth.Query})
	query := marshal(map[string]any{"context": ctx, "resources": []string{s.Table}, "where": map[string]any{"group": synth.Group}})
	rows := make([][]byte, spec.Calls)
	wide := make([][]byte, spec.Calls)
	for i := range spec.Calls {
		rows[i] = marshal(map[string]any{"context": ctx, "ref": s.Table,
			"selector": map[string]any{"selector_profile": "aavanam/row/v1", "index": (i * 7919) % s.Shape.TableRows}})
		wide[i] = marshal(map[string]any{"context": ctx, "ref": s.Wide[i%len(s.Wide)]})
	}
	runs := []struct {
		name  string
		op    string
		body  func(i int) []byte
		check measure.Check
	}{
		{measure.OpenContext, "open_context", func(int) []byte { return open }, ok},
		{measure.ReadRow, "read", func(i int) []byte { return rows[i] }, ok},
		{measure.ReadArtifact, "read", func(i int) []byte { return wide[i] }, ok},
		{measure.SearchText, "search_text", func(int) []byte { return search }, count(spec.Matches, false)},
		{"query_data_where", "query_data", func(int) []byte { return query }, count(s.GroupRows, true)},
	}
	var out []measure.Result
	for _, r := range runs {
		result, err := measure.Concurrent(spec.Prefix+r.name, spec.Clients, spec.Calls, spec.Limit,
			func(i int) ([]byte, error) { return call(r.op, r.body(i)) }, r.check)
		if err != nil {
			return out, err
		}
		if spec.Clients == 1 {
			result.Target = measure.Target(spec.Shape, r.name)
		}
		out = append(out, result)
	}
	return out, nil
}
