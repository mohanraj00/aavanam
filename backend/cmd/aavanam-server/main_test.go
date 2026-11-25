package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/mohanraj00/aavanam/backend/core/canon"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/memstore"
	"github.com/mohanraj00/aavanam/backend/core/storagetest"
)

// folder returns a folder with a short path: the path of a Unix socket has a small length limit.
func folder(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "av")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func options1(t *testing.T, args ...string) *options {
	t.Helper()
	o, err := parse(append([]string{"--read-addr", "127.0.0.1:0"}, args...), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	return o
}

// over returns a client that sends each request to a Unix socket.
func over(socket string) *http.Client {
	return &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}}
}

func post(t *testing.T, client *http.Client, url, body string) (int, string) {
	t.Helper()
	response, err := client.Post(url, "application/json", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	text, _ := io.ReadAll(response.Body)
	return response.StatusCode, string(text)
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

// field returns a string field of the first item of an envelope.
func field(t *testing.T, body, name string) string {
	t.Helper()
	e, err := jsontext.Parse([]byte(body), jsontext.Strict)
	if err != nil {
		t.Fatalf("the body is not JSON: %q", body)
	}
	items, _ := e.Get("items")
	if len(items.Arr) == 0 {
		t.Fatalf("the result has no items: %s", body)
	}
	value, _ := items.Arr[0].Get(name)
	return value.Str
}

// The service declares its configuration, has the read binding on TCP and each operation on the socket,
// and asks the validator of a policy at its address (NET-TRN-5, PUB-GAT-1, PUB-SEC-1, PUB-RCP-1).
func TestService(t *testing.T) {
	asked := 0
	validator := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		asked++
		_, _ = w.Write([]byte(`{"accepted":true}`))
	}))
	defer validator.Close()
	socket := filepath.Join(folder(t), "w.sock")
	s, err := start(context.Background(), options1(t, "--write-socket", socket, "--gate", "gated", "--section", "public=serving",
		"--policy", "pol="+validator.URL+"/decide?x=1"), memstore.New())
	if err != nil {
		t.Fatal(err)
	}
	defer s.stop()
	info, err := os.Lstat(socket)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode() != os.ModeSocket|0o600 {
		t.Fatalf("the write socket must have mode 0600: %v", info.Mode())
	}
	write, base := over(socket), "http://socket/v1/op/"

	const profile = `"descriptor_profile":"aavanam/base/v1"`
	set := `{` + profile + `,"kind":"binding-set"}`
	status, body := post(t, write, base+"put", storagetest.Put([]string{set, storagetest.Source("alpha")}, "alpha"))
	if status != 200 || !strings.Contains(body, `"status":"ok"`) {
		t.Fatalf("a put on the socket: HTTP %d %s", status, body)
	}
	unit := `{` + profile + `,"kind":"knowledge-unit","binding_set":"` + field(t, body, "ref") + `"}`
	_, body = post(t, write, base+"put", storagetest.Put([]string{unit}))
	units := `"units":["` + field(t, body, "ref") + `"]`

	// The section of the flag applies: a serving snapshot with a source in its closure is refused (PUB-SEC-3).
	observation := `{` + profile + `,"kind":"observation","subjects":["` + jsontext.Digest(canonical(t, storagetest.Source("alpha"))) +
		`"],"observation_type":"t","scope":{},"values":{}}`
	_, body = post(t, write, base+"put", storagetest.Put([]string{observation}))
	observations := `"observations":["` + field(t, body, "ref") + `"]`
	if _, body := post(t, write, base+"create_snapshot", `{"collection":"public",`+observations+`}`); !strings.Contains(body, "section_violation") {
		t.Errorf("a source in a serving collection of the flag: %s", body)
	}
	if _, body := post(t, write, base+"create_snapshot", `{"collection":"other",`+observations+`}`); !strings.Contains(body, `"status":"ok"`) {
		t.Errorf("a source in a collection with no section: %s", body)
	}

	// The gate of the flag needs an activation.
	_, body = post(t, write, base+"create_snapshot", `{"collection":"gated",`+units+`}`)
	gated := field(t, body, "ref")
	_, body = post(t, write, base+"create_snapshot", `{"collection":"open",`+units+`}`)
	open := field(t, body, "ref")
	receipt := `{` + profile + `,"kind":"receipt","producer":"p","payload":{"digest":"` + jsontext.Digest([]byte("r")) + `","media_type":"text/plain","size":1}}`
	_, body = post(t, write, base+"put", storagetest.Put([]string{receipt}, "r"))
	receiptRef := field(t, body, "ref")
	publish := func(snapshot string) string {
		_, body := post(t, write, base+"create_candidate", `{"snapshot":"`+snapshot+`","policy":"pol","expected_head":null}`)
		_, body = post(t, write, base+"publish", `{"candidate":"`+field(t, body, "candidate")+`","receipt":"`+receiptRef+`"}`)
		return body
	}
	if body := publish(gated); !strings.Contains(body, "activation_required") || asked != 0 {
		t.Errorf("a publish in the gated collection of the flag: %s, %d validator calls", body, asked)
	}
	if body := publish(open); field(t, body, "disposition") != "published" || asked != 1 {
		t.Errorf("a publish with the validator of the flag: %s, %d validator calls", body, asked)
	}

	// The read listener has the read binding only.
	if status, body := post(t, http.DefaultClient, s.ReadURL+"/v1/op/publications", `{"collection":"open"}`); status != 200 || field(t, body, "head") != open {
		t.Errorf("a read on the read listener: HTTP %d %s", status, body)
	}
	if status, _ := post(t, http.DefaultClient, s.ReadURL+"/v1/op/put", storagetest.Put([]string{set})); status != 404 {
		t.Errorf("a put on the read listener: HTTP %d, not 404 (NET-SCP-3)", status)
	}
	response, err := http.Get(s.ReadURL + "/v1/about")
	if err != nil || response.StatusCode != 200 {
		t.Errorf("the description: %v %v", response, err)
	}
	if response != nil {
		response.Body.Close()
	}
	if !strings.HasPrefix(s.ReadURL, "http://127.0.0.1:") {
		t.Errorf("the read listener is not on the loopback address: %s", s.ReadURL)
	}
}

// A system with a socket lock can replace a socket from an earlier run.
// A system with no socket lock asks the operator to remove the file.
func TestSocketPath(t *testing.T) {
	dir := folder(t)
	socket := filepath.Join(dir, "w.sock")
	old, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	old.(*net.UnixListener).SetUnlinkOnClose(false)
	old.Close()
	o := options1(t, "--write-socket", socket)
	if runtime.GOOS == "aix" || runtime.GOOS == "solaris" {
		s, err := start(context.Background(), o, memstore.New())
		if err == nil {
			s.stop()
			t.Fatal("the service started with an existing socket and no socket lock")
		}
		if !strings.Contains(err.Error(), "Make sure that no service uses the socket. Then remove the file") {
			t.Fatalf("the error must tell the operator how to remove the socket: %v", err)
		}
		if err := os.Remove(socket); err != nil {
			t.Fatalf("remove the socket from the earlier run: %v", err)
		}
	}
	type result struct {
		service *service
		err     error
	}
	started := make(chan result, 2)
	for range 2 {
		go func() {
			s, err := start(context.Background(), o, memstore.New())
			started <- result{s, err}
		}()
	}
	first, second := <-started, <-started
	if first.err != nil {
		first, second = second, first
	}
	if first.err != nil {
		t.Fatalf("the two starts failed: %v and %v", first.err, second.err)
	}
	if second.err == nil {
		second.service.stop()
		t.Error("two starts took the same socket")
	}
	s := first.service
	if status, body := post(t, over(socket), "http://socket/v1/op/publications", `{"collection":"c"}`); status != 200 || !strings.Contains(body, `"status":"ok"`) {
		t.Errorf("the first service after the second start: HTTP %d %s", status, body)
	}
	s.stop()
	if _, err := os.Lstat(socket); err == nil {
		t.Error("the socket stays after the stop of its service")
	}
	// A name that is not an absolute file path is refused: on Linux `@name` is an abstract socket, which
	// has no file mode. A listener that is not a socket file of one user is also refused.
	for _, name := range []string{"@aavanam-test", "relative.sock", "\x00aavanam-test", filepath.Join(dir, "a\x00b")} {
		if second, err := start(context.Background(), options1(t, "--write-socket", name), memstore.New()); err == nil {
			second.stop()
			t.Errorf("a start with the socket name %q", name)
		}
	}
	file := filepath.Join(dir, "data")
	if err := os.WriteFile(file, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := start(context.Background(), options1(t, "--write-socket", file), memstore.New()); err == nil {
		t.Error("a start with a file that is not a socket at the path")
	}
	if data, _ := os.ReadFile(file); string(data) != "keep" {
		t.Error("the start removed a file that is not a socket")
	}
	if err := os.Symlink(file, socket); err != nil {
		t.Fatal(err)
	}
	if s, err := start(context.Background(), o, memstore.New()); err == nil {
		s.stop()
		t.Error("a start followed a symbolic link at the socket path")
	}
	if target, err := os.Readlink(socket); err != nil || target != file {
		t.Errorf("the start changed the symbolic link: %q %v", target, err)
	}
}

func TestConfigurationThatIsNotCorrect(t *testing.T) {
	for _, args := range [][]string{{"--section", "c"}, {"--section", "=serving"}, {"--policy", "pol"}, {"--policy", "pol="},
		{"--deadline", "0s"}, {"--max-body-bytes", "0"}, {"--context-life", "-1s"}, {"--unknown"}, {"extra"},
		{"--read-socket", ""}, {"--read-socket-mode", "0660"}, {"--read-socket", "/read.sock", "--read-addr", "127.0.0.1:0"},
		{"--read-socket", "/read.sock", "--read-socket-mode", "0880"}, {"--read-socket", "/read.sock", "--read-socket-mode", "1660"},
		{"--read-socket", "/read.sock", "--read-socket-mode", "-660"}} {
		if _, err := parse(args, io.Discard); err == nil {
			t.Errorf("the arguments %v are not correct", args)
		}
	}
	for _, args := range [][]string{{"--section", "c=other"}, {"--policy", "pol=ftp://host/x"}, {"--policy", "pol=http://user:secret@host/x"},
		{"--section", "c=serving", "--section", "c=evidence"}, {"--read-addr", "not an address"}} {
		o, err := parse(args, io.Discard)
		if err != nil {
			t.Errorf("%v: %v", args, err)
			continue
		}
		if o.readAddr == "127.0.0.1:8437" {
			o.readAddr = "127.0.0.1:0"
		}
		s, err := start(context.Background(), o, memstore.New())
		if err == nil {
			s.stop()
			t.Errorf("the configuration %v is not correct", args)
		} else if strings.Contains(err.Error(), "secret") {
			t.Errorf("the error has a part of the address: %v", err)
		}
	}
	if o, err := parse(nil, io.Discard); err != nil || o.readAddr != "127.0.0.1:8437" || o.readSocket != "" ||
		o.readSocketMode != 0o600 || o.writeSocket != "" {
		t.Errorf("the default configuration: %+v %v", o, err)
	}
}

// contexts is a storage that records the configuration and the removals of its read contexts.
type contexts struct {
	*memstore.Store
	life    chan time.Duration
	removed chan time.Duration
}

func (c contexts) SetContextLife(life time.Duration) { c.life <- life }

func (c contexts) ExpireContexts(_ context.Context, grace time.Duration) (int64, error) {
	select {
	case c.removed <- grace:
	default:
	}
	return 0, nil
}

// The service gives the life of a read context to its storage and removes the old contexts again and again.
// The grace time of a removal is longer than the deadline of a call.
func TestContextLife(t *testing.T) {
	storage := contexts{memstore.New(), make(chan time.Duration, 1), make(chan time.Duration, 1)}
	s, err := start(context.Background(), options1(t, "--context-life", "200ms", "--deadline", "3s"), storage)
	if err != nil {
		t.Fatal(err)
	}
	defer s.stop()
	if life := <-storage.life; life != 200*time.Millisecond {
		t.Errorf("the life of a context: %v", life)
	}
	select {
	case grace := <-storage.removed:
		if grace <= 3*time.Second {
			t.Errorf("the grace time %v is not longer than the deadline of a call", grace)
		}
	case <-time.After(5 * time.Second):
		t.Error("the service did not remove the old contexts")
	}
	none := contexts{memstore.New(), make(chan time.Duration, 1), make(chan time.Duration, 1)}
	s2, err := start(context.Background(), options1(t, "--context-life", "0"), none)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.stop()
	select {
	case <-none.life:
		t.Error("a service with no context life gave one to its storage")
	case <-time.After(100 * time.Millisecond):
	}
}

// LYR-PRF-1 and IDX-PRF-1: the service declares the profiles of --profile before the first call. With no
// such option, it is a store of the base profile only.
func TestDeclaredProfiles(t *testing.T) {
	const layer = `{"descriptors":["{\"descriptor_profile\":\"aavanam/base/v1\",\"kind\":\"layer\",\"base\":\"c\",\"scope\":\"s\"}"]}`
	run := func(args ...string) (put, search string) {
		t.Helper()
		socket := filepath.Join(folder(t), "w.sock")
		s, err := start(context.Background(), options1(t, append([]string{"--write-socket", socket}, args...)...), memstore.New())
		if err != nil {
			t.Fatal(err)
		}
		defer s.stop()
		_, put = post(t, over(socket), "http://socket/v1/op/put", layer)
		_, search = post(t, over(socket), "http://socket/v1/op/semantic_search", `{}`)
		return put, search
	}
	if put, search := run(); !strings.Contains(put, "unknown_kind") || !strings.Contains(search, "reserved_profile") {
		t.Errorf("a service with no profile: %s %s", put, search)
	}
	if put, search := run("--profile", "aavanam/layer/v1"); !strings.Contains(put, `"status":"ok"`) || !strings.Contains(search, "reserved_profile") {
		t.Errorf("a service with the layer profile: %s %s", put, search)
	}
	put, search := run("--profile", "aavanam/layer/v1", "--profile", "aavanam/semantic-index/v1")
	if !strings.Contains(put, `"status":"ok"`) || !strings.Contains(search, "field_count") {
		t.Errorf("a service with the two profiles: %s %s", put, search)
	}
	if _, err := start(context.Background(), options1(t, "--profile", "aavanam/workspace/v1"), memstore.New()); err == nil {
		t.Error("a profile that the service does not have gave no error")
	}
}
