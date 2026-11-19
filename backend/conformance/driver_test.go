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
