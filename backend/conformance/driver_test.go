package conformance

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// script is a store that records its calls and asks the validator for each `publish`.
type script struct {
	Null
	calls []string
	asked Decision
}

func (s *script) Reset() error { s.calls = append(s.calls, "reset"); return nil }

func (s *script) Call(store string, operation, request json.RawMessage, validate Validate) (json.RawMessage, error) {
	s.calls = append(s.calls, "call "+store+" "+string(operation)+" "+string(request))
	switch string(operation) {
	case `"publish"`:
		d, err := validate(json.RawMessage(`"p"`), json.RawMessage(`"k"`), json.RawMessage(`"r"`), nil)
		if err != nil {
			return nil, err
		}
		s.asked = d
	case `"broken"`:
		return nil, errors.New("no such store")
	}
	return Unsupported(operation), nil
}

func (s *script) Declare(profiles []string) error {
	s.calls = append(s.calls, "declare "+strings.Join(profiles, " "))
	if len(profiles) == 0 {
		return errors.New("no profile")
	}
	return nil
}

func (s *script) Gate(c json.RawMessage) error {
	s.calls = append(s.calls, "gate "+string(c))
	return nil
}

func serve(t *testing.T, s Store, input string) []string {
	t.Helper()
	var out strings.Builder
	if err := Serve(strings.NewReader(input), &out, s, Hello{Ready: true}); err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
}

func TestServeKeepsTheRawValues(t *testing.T) {
	s := &script{}
	// A lone surrogate escape and a number form must reach the store with no change.
	lines := serve(t, s, `{"cmd":"reset"}`+"\n"+
		`{"cmd":"call","store":"writer","op":"get","request":{"q":"\ud800","n":1.0,"a":1,"a":2}}`+"\n"+
		`{"cmd":"gate","collection":"c\u0000"}`+"\n"+
		`{"cmd":"fingerprint","store":"output"}`)
	want := []string{`{"ready":true}`, `{"ok":true}`,
		`{"result":{"status":"unsupported","reasons":[{"code":"unsupported_operation","refs":[],"capabilities":[]}],"operation":"get","operation_profile":"aavanam/base/v1","context":null}}`,
		`{"ok":true}`, `{"fingerprint":""}`}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("lines:\n%s", strings.Join(lines, "\n"))
	}
	calls := []string{"reset", `call writer "get" {"q":"\ud800","n":1.0,"a":1,"a":2}`, `gate "c\u0000"`}
	if strings.Join(s.calls, "\n") != strings.Join(calls, "\n") {
		t.Fatalf("calls:\n%s", strings.Join(s.calls, "\n"))
	}
}

func TestServeAsksTheValidator(t *testing.T) {
	for answer, want := range map[string]Decision{`{"accepted":true}`: Accepted, `{"accepted":false}`: Rejected, `{"no_answer":true}`: NoAnswer} {
		s := &script{}
		lines := serve(t, s, `{"cmd":"call","store":"writer","op":"publish","request":{}}`+"\n"+answer+"\n")
		if len(lines) != 3 || lines[1] != `{"validate":{"policy":"p","candidate":"k","receipt":"r","activation":null}}` ||
			!strings.HasPrefix(lines[2], `{"result":`) {
			t.Fatalf("%s: lines %q", answer, lines)
		}
		if s.asked != want {
			t.Fatalf("%s: decision %d, not %d", answer, s.asked, want)
		}
	}
	// An answer with no decision is an error, not a rejection and not an acceptance.
	lines := serve(t, &script{}, `{"cmd":"call","store":"writer","op":"publish","request":{}}`+"\n"+`{}`+"\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[2], `{"error":`) {
		t.Fatalf("lines %q", lines)
	}
}

func TestServeGivesAnErrorLineAndContinues(t *testing.T) {
	input := strings.Join([]string{`not json`, `{"cmd":"x"}`, `{"cmd":"call","store":"cloud","op":"get","request":{}}`,
		`{"cmd":"call","store":"writer","op":"get"}`, `{"cmd":"fingerprint"}`, `{"cmd":"accept"}`, `{"cmd":"gate"}`,
		`{"cmd":"section","collection":"c"}`, `{"cmd":"call","store":"writer","op":"broken","request":{}}`, ``,
		`{"cmd":"section","collection":"c","section":"serving"}`}, "\n")
	lines := serve(t, &script{}, input)
	if len(lines) != 11 {
		t.Fatalf("%d lines: %q", len(lines), lines)
	}
	for i, line := range lines[1:10] {
		if !strings.HasPrefix(line, `{"error":`) {
			t.Fatalf("line %d is not an error line: %s", i+1, line)
		}
	}
	if lines[9] != `{"error":"no such store"}` || lines[10] != `{"ok":true}` {
		t.Fatalf("lines %q", lines[9:])
	}
}

// The command `declare` gives the profiles to the store (LYR-PRF-1, IDX-PRF-1). A store that refuses the
// profiles gives an error line.
func TestServeDeclaresProfiles(t *testing.T) {
	s := &script{}
	lines := serve(t, s, `{"cmd":"declare","profiles":["aavanam/layer/v1","aavanam/semantic-index/v1"]}`+"\n"+`{"cmd":"declare"}`)
	want := []string{`{"ready":true}`, `{"ok":true}`, `{"error":"no profile"}`}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("lines:\n%s", strings.Join(lines, "\n"))
	}
	if got := strings.Join(s.calls, "; "); got != "declare aavanam/layer/v1 aavanam/semantic-index/v1; declare " {
		t.Errorf("calls: %q", got)
	}
}

// rawScript is a store with the raw bundle form. It records the bytes of each raw import.
type rawScript struct {
	script
	imported [][]byte
}

func (s *rawScript) ImportRaw(store string, raw []byte, _ Validate) (json.RawMessage, error) {
	s.calls = append(s.calls, "raw "+store)
	s.imported = append(s.imported, raw)
	return Unsupported(json.RawMessage(`"import"`)), nil
}

func (s *rawScript) RawBundle(bundle json.RawMessage) ([]byte, error) {
	if string(bundle) == `{}` {
		return nil, errors.New("not a bundle")
	}
	return []byte("line\n\x00\xff"), nil
}

// A call with `raw` gives the bytes of the base64 text to the raw import of the store (BUN-RAW-4). The
// command `raw_bundle` gives the raw form of a bundle as base64 text (BUN-RAW-3). A store with no raw
// bundle form gives an error line.
func TestServeRawBundle(t *testing.T) {
	s := &rawScript{}
	input := strings.Join([]string{
		`{"cmd":"call","store":"device","op":"import","raw":"bGluZQoA/w=="}`,
		`{"cmd":"call","store":"device","op":"import","raw":""}`,
		`{"cmd":"raw_bundle","bundle":{"format":"x"}}`,
		`{"cmd":"call","store":"device","op":"import","raw":"not base64"}`,
		`{"cmd":"call","store":"device","op":"get","raw":"bGluZQ=="}`,
		`{"cmd":"call","store":"cloud","op":"import","raw":"bGluZQ=="}`,
		`{"cmd":"raw_bundle"}`,
		`{"cmd":"raw_bundle","bundle":{}}`,
	}, "\n")
	lines := serve(t, s, input)
	if len(lines) != 9 {
		t.Fatalf("%d lines: %q", len(lines), lines)
	}
	if len(s.imported) != 2 || string(s.imported[0]) != "line\n\x00\xff" || len(s.imported[1]) != 0 {
		t.Fatalf("the raw imports: %q", s.imported)
	}
	if strings.Join(s.calls, ",") != "raw device,raw device" {
		t.Fatalf("the calls: %q", s.calls)
	}
	for _, line := range lines[1:3] {
		if !strings.HasPrefix(line, `{"result":`) {
			t.Fatalf("a raw import gave no result: %s", line)
		}
	}
	if lines[3] != `{"raw":"bGluZQoA/w=="}` {
		t.Fatalf("the raw form: %s", lines[3])
	}
	for i, line := range lines[4:] {
		if !strings.HasPrefix(line, `{"error":`) {
			t.Fatalf("line %d is not an error line: %s", i+4, line)
		}
	}
	for _, line := range serve(t, &script{}, `{"cmd":"call","store":"device","op":"import","raw":"bGluZQ=="}`+"\n"+`{"cmd":"raw_bundle","bundle":{"a":1}}`)[1:] {
		if !strings.HasPrefix(line, `{"error":`) {
			t.Fatalf("a store with no raw bundle form gave: %s", line)
		}
	}
}
