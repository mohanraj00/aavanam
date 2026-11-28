// Package conformance has the line protocol between the Python vector runner and a Go driver.
//
// A driver reads one JSON object for each line on its input and writes one JSON object for each line on its
// output (docs/design/backend.md, section 6). The adapter is conformance/backend_adapter.py.
//
// The protocol keeps each value of a request as raw JSON. The standard decoder changes a lone surrogate
// escape into U+FFFD, and that change can hide a failed check (STS-MAL-3). Thus the store decodes each value
// with its own strict reader.
package conformance

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Profile is the operation profile of each result envelope (STS-ENV-1).
const Profile = "aavanam/base/v1"

// Decision is the output of a validator (PUB-RCP-2), or no output (PUB-RCP-4).
type Decision int

const (
	// Rejected: the validator does not accept the receipt.
	Rejected Decision = iota
	// Accepted: the validator accepts the receipt.
	Accepted
	// NoAnswer: the validator did not answer before its deadline (PUB-RCP-4).
	NoAnswer
)

// Validate asks the adapter for the decision of a policy. Each argument is a raw JSON value.
// The activation is the JSON value null if the request has none.
type Validate func(policy, candidate, receipt, activation json.RawMessage) (Decision, error)

// Store is the implementation that a driver connects to the runner. Each string argument that comes
// from a vector is a raw JSON value. Call returns the result envelope as one JSON text.
type Store interface {
	// Reset makes each store empty and removes each policy, gate and section. After it, a store declares
	// no profile.
	Reset() error
	// Declare makes each store declare the profiles (LYR-PRF-1, IDX-PRF-1). The adapter calls it after
	// Reset, so each store is empty. A store that does not have a profile gives an error.
	Declare(profiles []string) error
	// Call sends one request to the store with the role `store`: "writer", "device" or "output".
	Call(store string, operation, request json.RawMessage, validate Validate) (json.RawMessage, error)
	// Fingerprint returns a text that changes if the content of the store changes.
	Fingerprint(store string) (string, error)
	// Accept registers an acceptance policy (PUB-RCP-1). The store asks `validate` of Call for each decision.
	Accept(policy json.RawMessage) error
	// Gate declares a gated collection (PUB-GAT-1).
	Gate(collection json.RawMessage) error
	// Section declares the storage section of a collection (PUB-SEC-1).
	Section(collection, section json.RawMessage) error
	// Advance moves the clock of each store forward by a number of seconds (CTX-LIF-2). After Reset, the
	// clock of each store is the system clock.
	Advance(seconds int64) error
}

// Hello is the first line that a driver writes.
type Hello struct {
	Ready bool `json:"ready"`
	// ReadURL is the base URL of the network read binding (NET-TRN-1), or empty if the driver has none.
	ReadURL string `json:"read_url,omitempty"`
}

type message struct {
	Cmd        string          `json:"cmd"`
	Store      string          `json:"store"`
	Op         json.RawMessage `json:"op"`
	Request    json.RawMessage `json:"request"`
	Policy     json.RawMessage `json:"policy"`
	Collection json.RawMessage `json:"collection"`
	Section    json.RawMessage `json:"section"`
	Profiles   []string        `json:"profiles"`
	Advance    *int64          `json:"advance"`
	// Raw is the base64 text of a raw bundle for the operation `import`, in place of Request (BUN-RAW-4).
	// The base64 text is a part of this test protocol only.
	Raw *string `json:"raw"`
	// Bundle is the bundle of the command `raw_bundle` (BUN-RAW-3).
	Bundle json.RawMessage `json:"bundle"`
	// Path is the stored form of the command `restore`: a file or a folder of a release store.
	Path string `json:"path"`
}

// RawStore is a Store that has the raw bundle form (BUN-RAW-3, BUN-RAW-4).
type RawStore interface {
	// ImportRaw sends a raw bundle to the operation `import` of one store.
	ImportRaw(store string, raw []byte, validate Validate) (json.RawMessage, error)
	// RawBundle returns a raw form of a bundle.
	RawBundle(bundle json.RawMessage) ([]byte, error)
}

// Restorer is a Store that can make its stores from a stored form that an earlier release wrote (MIG-VER-1).
type Restorer interface {
	// Restore makes each store of the driver from the stored form at path. Each store keeps its declared
	// profiles. It returns false, and it changes no store, if the storage of the driver has no stored form.
	Restore(path string) (bool, error)
}

type answer struct {
	Accepted *bool `json:"accepted"`
	NoAnswer bool  `json:"no_answer"`
}

var stores = map[string]bool{"writer": true, "device": true, "output": true}

// Serve runs the protocol until the input ends. A failure of one command gives an "error" line, and the
// loop continues. Serve returns an error only if it cannot read or write a line.
func Serve(in io.Reader, out io.Writer, s Store, hello Hello) error {
	r := bufio.NewReaderSize(in, 1<<16)
	w := bufio.NewWriter(out)
	send := func(parts ...[]byte) error {
		for _, p := range parts {
			if _, err := w.Write(p); err != nil {
				return err
			}
		}
		if err := w.WriteByte('\n'); err != nil {
			return err
		}
		return w.Flush()
	}
	sendValue := func(v any) error {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		return send(b)
	}
	validate := func(policy, candidate, receipt, activation json.RawMessage) (Decision, error) {
		if len(activation) == 0 {
			activation = json.RawMessage("null")
		}
		if err := send([]byte(`{"validate":{"policy":`), policy, []byte(`,"candidate":`), candidate,
			[]byte(`,"receipt":`), receipt, []byte(`,"activation":`), activation, []byte(`}}`)); err != nil {
			return Rejected, err
		}
		line, err := r.ReadBytes('\n')
		if err != nil {
			return Rejected, fmt.Errorf("no answer line for a validator call: %w", err)
		}
		var a answer
		if err := json.Unmarshal(line, &a); err != nil {
			return Rejected, fmt.Errorf("the answer line of a validator call is not JSON: %w", err)
		}
		switch {
		case a.NoAnswer:
			return NoAnswer, nil
		case a.Accepted != nil && *a.Accepted:
			return Accepted, nil
		case a.Accepted != nil:
			return Rejected, nil
		}
		return Rejected, errors.New("the answer line of a validator call has no decision")
	}
	if err := sendValue(hello); err != nil {
		return err
	}
	for {
		line, err := r.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) == 0 {
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}
			continue
		}
		reply, failure := handle(s, line, validate)
		if failure != nil {
			if serr := sendValue(map[string]string{"error": failure.Error()}); serr != nil {
				return serr
			}
		} else if serr := send(reply...); serr != nil {
			return serr
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func handle(s Store, line []byte, validate Validate) ([][]byte, error) {
	var m message
	if err := json.Unmarshal(line, &m); err != nil {
		return nil, fmt.Errorf("the line is not a JSON object: %w", err)
	}
	ok := [][]byte{[]byte(`{"ok":true}`)}
	switch m.Cmd {
	case "reset":
		return ok, s.Reset()
	case "declare":
		return ok, s.Declare(m.Profiles)
	case "call":
		if m.Raw != nil {
			rawStore, has := s.(RawStore)
			raw, err := base64.StdEncoding.DecodeString(*m.Raw)
			if !has || !stores[m.Store] || string(m.Op) != `"import"` || err != nil {
				return nil, errors.New("a call with raw needs a store with the raw bundle form, the op import and base64 text")
			}
			result, err := rawStore.ImportRaw(m.Store, raw, validate)
			if err != nil {
				return nil, err
			}
			return [][]byte{[]byte(`{"result":`), result, []byte(`}`)}, nil
		}
		if !stores[m.Store] || len(m.Op) == 0 || len(m.Request) == 0 {
			return nil, errors.New("a call needs store, op and request")
		}
		result, err := s.Call(m.Store, m.Op, m.Request, validate)
		if err != nil {
			return nil, err
		}
		return [][]byte{[]byte(`{"result":`), result, []byte(`}`)}, nil
	case "raw_bundle":
		rawStore, has := s.(RawStore)
		if !has || len(m.Bundle) == 0 {
			return nil, errors.New("raw_bundle needs a store with the raw bundle form and a bundle")
		}
		raw, err := rawStore.RawBundle(m.Bundle)
		if err != nil {
			return nil, err
		}
		b, err := json.Marshal(map[string]string{"raw": base64.StdEncoding.EncodeToString(raw)})
		return [][]byte{b}, err
	case "fingerprint":
		if !stores[m.Store] {
			return nil, errors.New("a fingerprint needs store")
		}
		print, err := s.Fingerprint(m.Store)
		if err != nil {
			return nil, err
		}
		b, err := json.Marshal(map[string]string{"fingerprint": print})
		return [][]byte{b}, err
	case "accept":
		if len(m.Policy) == 0 {
			return nil, errors.New("accept needs policy")
		}
		return ok, s.Accept(m.Policy)
	case "gate":
		if len(m.Collection) == 0 {
			return nil, errors.New("gate needs collection")
		}
		return ok, s.Gate(m.Collection)
	case "section":
		if len(m.Collection) == 0 || len(m.Section) == 0 {
			return nil, errors.New("section needs collection and section")
		}
		return ok, s.Section(m.Collection, m.Section)
	case "restore":
		if m.Path == "" {
			return nil, errors.New("restore needs path")
		}
		restored := false
		if r, has := s.(Restorer); has {
			var err error
			if restored, err = r.Restore(m.Path); err != nil {
				return nil, err
			}
		}
		b, err := json.Marshal(map[string]bool{"restored": restored})
		return [][]byte{b}, err
	case "clock":
		if m.Advance == nil || *m.Advance < 0 {
			return nil, errors.New("clock needs advance, a number of seconds that is 0 or more")
		}
		return ok, s.Advance(*m.Advance)
	}
	return nil, fmt.Errorf("unknown cmd %q", m.Cmd)
}

// Unsupported returns the envelope of an operation that the store does not have (STS-MAL-4).
// `operation` is the raw JSON value of the operation name.
func Unsupported(operation json.RawMessage) json.RawMessage {
	var b bytes.Buffer
	b.WriteString(`{"status":"unsupported","reasons":[{"code":"unsupported_operation","refs":[],"capabilities":[]}],"operation":`)
	b.Write(operation)
	b.WriteString(`,"operation_profile":"` + Profile + `","context":null}`)
	return b.Bytes()
}

// Null is the null implementation: each call gives `unsupported` with `unsupported_operation`.
// It must fail the vectors, as conformance/null_adapter.py does.
type Null struct{}

func (Null) Reset() error           { return nil }
func (Null) Declare([]string) error { return nil }

func (Null) Call(_ string, operation, _ json.RawMessage, _ Validate) (json.RawMessage, error) {
	return Unsupported(operation), nil
}

func (Null) Fingerprint(string) (string, error) { return "", nil }
func (Null) Accept(json.RawMessage) error       { return nil }
func (Null) Gate(json.RawMessage) error         { return nil }
func (Null) Section(_, _ json.RawMessage) error { return nil }
func (Null) Advance(int64) error                { return nil }
