package api_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mohanraj00/aavanam/backend/api"
	"github.com/mohanraj00/aavanam/backend/core/canon"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/kinds"
	"github.com/mohanraj00/aavanam/backend/core/memstore"
	"github.com/mohanraj00/aavanam/backend/core/ops"
	"github.com/mohanraj00/aavanam/backend/core/read"
	"github.com/mohanraj00/aavanam/backend/core/storagetest"
	textsearch "github.com/mohanraj00/aavanam/backend/core/text"
	"github.com/mohanraj00/aavanam/backend/core/transfer"
	"github.com/mohanraj00/aavanam/backend/core/write"
)

type service struct {
	t     *testing.T
	mem   *memstore.Store
	store *ops.Store
	read  *httptest.Server
}

// registry returns a registry with each operation that the service has.
func registry() *ops.Registry {
	r := ops.NewRegistry()
	read.Register(r)
	textsearch.Register(r)
	transfer.Register(r)
	write.Register(r)
	return r
}

func start(t *testing.T, storage ops.Storage, config api.Config) *service {
	t.Helper()
	mem := memstore.New()
	if storage == nil {
		storage = mem
	}
	s := &service{t: t, mem: mem, store: ops.New(ops.Writer, storage, registry())}
	config.Store = func() *ops.Store { return s.store }
	s.read = httptest.NewServer(api.Read(config))
	t.Cleanup(s.read.Close)
	return s
}

// do sends one request and returns the HTTP status, the content type and the body.
func (s *service) do(method, path string, body io.Reader) (int, string, string) {
	s.t.Helper()
	request, err := http.NewRequest(method, s.read.URL+path, body)
	if err != nil {
		s.t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		s.t.Fatal(err)
	}
	defer response.Body.Close()
	text, err := io.ReadAll(response.Body)
	if err != nil {
		s.t.Fatal(err)
	}
	return response.StatusCode, response.Header.Get("Content-Type"), string(text)
}

func (s *service) direct(operation, request string) string {
	return string(s.store.Call(context.Background(), jsontext.S(operation), []byte(request)).JSON())
}

// parse returns the JSON object of a response body.
func parse(t *testing.T, body string) jsontext.Value {
	t.Helper()
	value, err := jsontext.Parse([]byte(body), jsontext.Strict)
	if err != nil || value.Kind != jsontext.Object {
		t.Fatalf("the body is not a JSON object: %q", body)
	}
	return value
}

// text returns the string of a field of an object.
func text(object jsontext.Value, field string) string {
	value, _ := object.Get(field)
	return value.Str
}

// canonical returns the canonical form of a descriptor text (IDN-CAN-1).
func canonical(t *testing.T, descriptor string) []byte {
	t.Helper()
	value, err := jsontext.Parse([]byte(descriptor), jsontext.Strict)
	if err != nil {
		t.Fatal(err)
	}
	return canon.Canonical(value)
}

// NET-TRN-1 to NET-TRN-3 and NET-SCP-1: a call gives HTTP 200 and the envelope of a direct call of the store
// with the same bytes, also for a body that is not correct JSON and for a result that is not `ok`.
func TestCallGivesTheEnvelopeOfTheStore(t *testing.T) {
	s := start(t, nil, api.Config{})
	for _, call := range [][2]string{
		{"publications", `{"collection":"c"}`},
		{"publications", `{"collection":"c","collection":"d"}`}, // duplicate_key
		{"publications", "{\"collection\":\"\xff\"}"},           // malformed_json
		{"publications", `{"collection":"\ud800"}`},             // a lone surrogate goes to the operation
		{"publications", ``},
		{"publications", `[]`},
		{"get", `{"context":"ctx-0","refs":["sha256:` + strings.Repeat("0", 64) + `"]}`},
		{"open_context", `{"heads":["c"]}`},
		{"export", `{}`}, // an operation of the binding that this store does not have (NET-CMP-2)
	} {
		status, kind, body := s.do("POST", "/v1/op/"+call[0], strings.NewReader(call[1]))
		if want := s.direct(call[0], call[1]); status != 200 || kind != "application/json" || body != want {
			t.Errorf("%s %q: HTTP %d %s\n got  %s\n want %s", call[0], call[1], status, kind, body, want)
		}
	}
	_, _, body := s.do("POST", "/v1/op/publications", strings.NewReader(`{"collection":"c","collection":"d"}`))
	if !strings.Contains(body, `"duplicate_key"`) || !strings.Contains(body, `"status":"failed"`) {
		t.Errorf("a duplicate key (NET-TRN-2): %s", body)
	}
}

// NET-SCP-2: a name with no row gives the envelope of STS-MAL-4, and the server does not check the body.
func TestNameThatIsNotInTheBinding(t *testing.T) {
	s := start(t, nil, api.Config{MaxBodyBytes: 8})
	for name, reason := range map[string]string{"sample_operation": "unsupported_operation", "lease_acquire": "reserved_profile",
		"a%20b": "unsupported_operation", "%E2%82%AC": "unsupported_operation"} {
		status, kind, body := s.do("POST", "/v1/op/"+name, strings.NewReader("this body is not JSON, and it is larger than the limit"))
		if status != 200 || kind != "application/json" || !strings.Contains(body, `"status":"unsupported"`) || !strings.Contains(body, reason) {
			t.Errorf("%s: HTTP %d %s", name, status, body)
		}
	}
}

// NET-SCP-3 and NET-TRN-4: an operation that writes content gives HTTP 404 with no envelope, and the server
// does not run it. A different path or method gives 404 or 405.
func TestRequestsThatAreNotCallsOfTheBinding(t *testing.T) {
	s := start(t, nil, api.Config{})
	before := s.mem.Fingerprint()
	put := storagetest.Put([]string{storagetest.Source("alpha")}, "alpha")
	for _, c := range []struct {
		method, path, body string
		want               int
	}{
		{"POST", "/v1/op/put", put, 404},
		{"POST", "/v1/op/create_snapshot", `{"collection":"c"}`, 404},
		{"POST", "/v1/op/create_candidate", `{}`, 404},
		{"POST", "/v1/op/publish", `{}`, 404},
		{"POST", "/v1/op/import", `{}`, 404},
		{"POST", "/v1/op/execution_inputs", `{}`, 404},
		{"POST", "/v1/op/build_inputs", `{}`, 404},
		{"POST", "/v1/op/producer_put", `{"producer":"p","key":"k","data":""}`, 404},
		{"POST", "/v1/op/producer_get", `{"producer":"p","key":"k"}`, 404},
		{"POST", "/v1/op/producer_forget", `{}`, 404},
		{"POST", "/v1/op/producer_removals", `{}`, 404},
		{"POST", "/v1/op/producer_append", `{}`, 404},
		{"POST", "/v1/op/producer_read", `{}`, 404},
		{"POST", "/v1/op/remove", `{}`, 404},
		{"POST", "/v1/op/removals", `{}`, 404},
		{"POST", "/v1/op/gc_collect", `{}`, 404},
		{"GET", "/v1/op/get", ``, 405},
		{"PUT", "/v1/op/get", `{}`, 405},
		{"POST", "/v1/about", `{}`, 405},
		{"POST", "/v1/op/", `{}`, 404},
		{"POST", "/v1/op/get/", `{}`, 404},
		{"POST", "/v1/op/get/more", `{}`, 404},
		{"POST", "/v1/op/%ff", `{}`, 404},
		{"POST", "/v1/op", `{}`, 404},
		{"GET", "/v1/about/", ``, 404},
		{"GET", "/v2/about", ``, 404},
		{"GET", "/", ``, 404},
		{"POST", "/v1/OP/get", `{}`, 404},
	} {
		status, kind, body := s.do(c.method, c.path, strings.NewReader(c.body))
		if status != c.want || strings.Contains(kind, "json") || strings.Contains(body, `"status"`) {
			t.Errorf("%s %s: HTTP %d %s %q, not %d with no envelope", c.method, c.path, status, kind, body, c.want)
		}
	}
	if s.mem.Fingerprint() != before {
		t.Error("the read binding changed the content of the store")
	}
}

// chunked is a body with no declared length.
type chunked struct{ io.Reader }

// NET-LIM-1: a body that is larger than the limit gives HTTP 413 with no envelope.
func TestBodyLimit(t *testing.T) {
	s := start(t, nil, api.Config{MaxBodyBytes: 64})
	fits := `{"collection":"` + strings.Repeat("c", 64-len(`{"collection":""}`)) + `"}`
	if status, _, body := s.do("POST", "/v1/op/publications", strings.NewReader(fits)); status != 200 || !strings.Contains(body, `"status":"ok"`) {
		t.Errorf("a body of the limit: HTTP %d %s", status, body)
	}
	large := `{"collection":"` + strings.Repeat("c", 64) + `"}`
	for name, body := range map[string]io.Reader{"a declared length": strings.NewReader(large), "no declared length": chunked{strings.NewReader(large)}} {
		if status, _, text := s.do("POST", "/v1/op/publications", body); status != 413 || strings.Contains(text, `"status"`) {
			t.Errorf("a large body with %s: HTTP %d %q", name, status, text)
		}
	}
}

// slow is a storage whose calls wait for the end of their context.
type slow struct{ ops.Storage }

func (s slow) Session(ctx context.Context) ops.Session {
	<-ctx.Done()
	return s.Storage.Session(ctx)
}

// NET-LIM-2: a call that does not complete before the deadline gives HTTP 200 and `deadline_exceeded` with
// no items. The envelope has the context of the request.
func TestDeadline(t *testing.T) {
	s := start(t, slow{memstore.New()}, api.Config{Deadline: 50 * time.Millisecond})
	started := time.Now()
	status, kind, body := s.do("POST", "/v1/op/get", strings.NewReader(`{"context":"ctx-1","refs":[]}`))
	e := parse(t, body)
	reasons, _ := e.Get("reasons")
	items, hasItems := e.Get("items")
	if status != 200 || kind != "application/json" || text(e, "status") != "failed" || text(e, "operation") != "get" ||
		text(e, "context") != "ctx-1" || len(reasons.Arr) != 1 || text(reasons.Arr[0], "code") != "deadline_exceeded" ||
		!hasItems || items.Kind != jsontext.Array || len(items.Arr) != 0 {
		t.Errorf("a call after the deadline: HTTP %d %s", status, body)
	}
	if time.Since(started) > 5*time.Second {
		t.Error("the response came long after the deadline")
	}
}

// NET-VER-1: the description of the API. The operations are the list of NET-SCP-1 in ascending string order.
func TestAbout(t *testing.T) {
	s := start(t, nil, api.Config{MaxBodyBytes: 1000, Deadline: 2 * time.Second})
	status, kind, body := s.do("GET", "/v1/about", nil)
	want := `{"api":"aavanam-read/v1","api_minor":1,"deadline_ms":2000,"max_body_bytes":1000,` +
		`"operations":["close_context","compare","context_bundle","coverage","evidence","export","get","impact","item_history","list_items",` +
		`"observations","open_context","publications","query_data","read","render","search","search_text","snapshot_changes"],` +
		`"profile":"aavanam/base/v1","profile_minor":14}`
	if status != 200 || kind != "application/json" || body != want {
		t.Errorf("the description: HTTP %d %s\n got  %s\n want %s", status, kind, body, want)
	}
}

// The listener for the writes has each operation of the table, and it does not have the description.
func TestWriteListener(t *testing.T) {
	mem := memstore.New()
	store := ops.New(ops.Writer, mem, registry())
	server := httptest.NewServer(api.Write(api.Config{Store: func() *ops.Store { return store }}))
	defer server.Close()
	post := func(path, body string) (int, string) {
		response, err := http.Post(server.URL+path, "application/json", bytes.NewReader([]byte(body)))
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		text, _ := io.ReadAll(response.Body)
		return response.StatusCode, string(text)
	}
	put := storagetest.Put([]string{storagetest.Source("alpha")}, "alpha")
	status, body := post("/v1/op/put", put)
	if e := parse(t, body); status != 200 || text(e, "status") != "ok" || text(e, "operation") != "put" {
		t.Errorf("a put: HTTP %d %s", status, body)
	}
	if _, _, has := mem.Session(context.Background()).Object(jsontext.Digest(canonical(t, storagetest.Source("alpha")))); !has {
		t.Error("the store does not have the descriptor of the put")
	}
	if status, body := post("/v1/op/publications", `{"collection":"c"}`); status != 200 || !strings.Contains(body, `"status":"ok"`) {
		t.Errorf("a read on the listener for the writes: HTTP %d %s", status, body)
	}
	if status, body := post("/v1/op/lease_acquire", `{}`); status != 200 || !strings.Contains(body, "reserved_profile") {
		t.Errorf("a reserved operation: HTTP %d %s", status, body)
	}
	response, err := http.Get(server.URL + "/v1/about")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 404 {
		t.Errorf("the description on the listener for the writes: HTTP %d", response.StatusCode)
	}
}

// NET-SCP-2, NET-SCP-3, RSV-LST-4 and IDX-PRF-4: `semantic_search` has a row with only `unsupported` for a
// store that does not declare the retrieval index profile. For a store that declares it, the row has more
// statuses, and the operation is not an operation of NET-SCP-1. Thus the read listener gives HTTP 404 and
// does not run it, and the listener for the writes runs it.
func TestOperationOfADeclaredProfile(t *testing.T) {
	s := start(t, nil, api.Config{MaxBodyBytes: 64})
	large := strings.Repeat("this body is not JSON ", 8)
	status, _, body := s.do("POST", "/v1/op/semantic_search", strings.NewReader(large))
	if status != 200 || !strings.Contains(body, "reserved_profile") || !strings.Contains(body, kinds.IndexProfile) {
		t.Errorf("a store with no profile: HTTP %d %s", status, body)
	}
	s.store = ops.New(ops.Writer, s.mem, registry())
	s.store.Profiles = kinds.Profiles{kinds.IndexProfile: true}
	if status, kind, body := s.do("POST", "/v1/op/semantic_search", strings.NewReader(`{}`)); status != 404 || kind == "application/json" {
		t.Errorf("the read listener of a store with the profile: HTTP %d %s", status, body)
	}
	_, _, about := s.do("GET", "/v1/about", nil)
	if strings.Contains(about, "semantic_search") {
		t.Errorf("the description has an operation that is not in the binding: %s", about)
	}
	server := httptest.NewServer(api.Write(api.Config{Store: func() *ops.Store { return s.store }}))
	defer server.Close()
	response, err := http.Post(server.URL+"/v1/op/semantic_search", "application/json", strings.NewReader(`{"context":"ctx-0"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	text, _ := io.ReadAll(response.Body)
	if response.StatusCode != 200 || !strings.Contains(string(text), `"field_count"`) {
		t.Errorf("the listener for the writes of a store with the profile: HTTP %d %s", response.StatusCode, text)
	}
}
