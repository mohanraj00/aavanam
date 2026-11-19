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
	"strings"
	"testing"

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
	if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0o600 {
		t.Fatalf("the socket of the write listener: %v %v", info.Mode(), err)
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

// A socket of an earlier run does not stop a start. A file of a different type does, and it stays.
func TestSocketPath(t *testing.T) {
	dir := folder(t)
	socket := filepath.Join(dir, "w.sock")
	old, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	old.(*net.UnixListener).SetUnlinkOnClose(false)
	old.Close()
	s, err := start(context.Background(), options1(t, "--write-socket", socket), memstore.New())
	if err != nil {
		t.Fatalf("a start with a socket of an earlier run: %v", err)
	}
	s.stop()
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
}

func TestConfigurationThatIsNotCorrect(t *testing.T) {
	for _, args := range [][]string{{"--section", "c"}, {"--section", "=serving"}, {"--policy", "pol"}, {"--policy", "pol="},
		{"--deadline", "0s"}, {"--max-body-bytes", "0"}, {"--unknown"}, {"extra"}} {
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
	if o, err := parse(nil, io.Discard); err != nil || o.readAddr != "127.0.0.1:8437" || o.writeSocket != "" {
		t.Errorf("the default configuration: %+v %v", o, err)
	}
}
