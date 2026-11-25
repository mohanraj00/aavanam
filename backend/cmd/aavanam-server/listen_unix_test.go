//go:build unix

package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/memstore"
	"github.com/mohanraj00/aavanam/backend/core/storagetest"
)

// The read API gives the same envelopes on the socket as on TCP (NET-TRN-3, NET-SCP-1, NET-SCP-3).
// The result of open_context has one item for the root (CTX-OPN-1).
func TestReadSocket(t *testing.T) {
	dir := folder(t)
	socket, writer := filepath.Join(dir, "r.sock"), filepath.Join(dir, "w.sock")
	o, err := parse([]string{"--read-socket", socket, "--read-socket-mode", "0660", "--write-socket", writer}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	storage := memstore.New()
	s, err := start(context.Background(), o, storage)
	if err != nil {
		t.Fatal(err)
	}
	defer s.stop()
	if len(s.listeners) != 2 || s.listeners[0].Addr().Network() != "unix" || s.listeners[1].Addr().Network() != "unix" {
		t.Fatalf("the service must have two Unix listeners: %v", s.listeners)
	}
	for path, mode := range map[string]os.FileMode{socket: 0o660, writer: 0o600} {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode() != os.ModeSocket|mode {
			t.Fatalf("the mode of %s is %v", path, info.Mode())
		}
	}
	tcp, err := start(context.Background(), options1(t), storage)
	if err != nil {
		t.Fatal(err)
	}
	defer tcp.stop()
	read, write := over(socket), over(writer)
	defer read.CloseIdleConnections()
	defer write.CloseIdleConnections()
	const base = "http://socket/v1/op/"
	const profile = `"descriptor_profile":"aavanam/base/v1"`
	set := `{` + profile + `,"kind":"binding-set"}`
	_, body := post(t, write, base+"put", storagetest.Put([]string{set}))
	unit := `{` + profile + `,"kind":"knowledge-unit","binding_set":"` + field(t, body, "ref") + `"}`
	_, body = post(t, write, base+"put", storagetest.Put([]string{unit}))
	root := field(t, body, "ref")
	status, body := post(t, read, base+"open_context", `{"roots":["`+root+`"]}`)
	opened, parseErr := jsontext.Parse([]byte(body), jsontext.Strict)
	contextID, _ := opened.Get("context")
	result, _ := opened.Get("status")
	items, _ := opened.Get("items")
	if status != http.StatusOK || parseErr != nil || result.Str != "ok" || !strings.HasPrefix(contextID.Str, "ctx-") || len(items.Arr) != 1 {
		t.Fatalf("open_context on the socket: HTTP %d %s", status, body)
	}
	ref, _ := items.Arr[0].Get("ref")
	if ref.Kind != jsontext.String || ref.Str != root {
		t.Fatalf("open_context returned ref %q, want %q: %s", ref.Str, root, body)
	}
	for _, call := range []struct {
		operation, request, outcome string
		status                      int
	}{
		{"get", `{"context":"` + contextID.Str + `","refs":["` + root + `"]}`, `"status":"ok"`, http.StatusOK},
		{"open_context", `{"heads":[],"heads":[]}`, `"code":"duplicate_key"`, http.StatusOK},
		{"put", storagetest.Put([]string{set}), "Not Found", http.StatusNotFound},
	} {
		status, body := post(t, read, base+call.operation, call.request)
		tcpStatus, tcpBody := post(t, http.DefaultClient, tcp.ReadURL+"/v1/op/"+call.operation, call.request)
		if status != call.status || !strings.Contains(body, call.outcome) || status != tcpStatus || body != tcpBody {
			t.Errorf("%s: Unix HTTP %d %s, TCP HTTP %d %s", call.operation, status, body, tcpStatus, tcpBody)
		}
	}
}

func TestSocketModeAtBind(t *testing.T) {
	for _, mode := range []os.FileMode{0o000, 0o600, 0o660, 0o777} {
		t.Run(fmt.Sprintf("%04o", mode), func(t *testing.T) {
			path := filepath.Join(folder(t), "read.sock")
			listener, err := bindSocket(path, mode)
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			info, err := os.Lstat(path)
			if err != nil || info.Mode() != os.ModeSocket|mode {
				t.Fatalf("the bind must create a socket with mode %04o: %v %v", mode, info, err)
			}
		})
	}
}

func TestSocketFileCheck(t *testing.T) {
	dir := folder(t)
	openTCP := func(string, os.FileMode) (net.Listener, error) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		return listener, nil
	}
	if listener, err := socketListener(filepath.Join(dir, "none.sock"), 0o600, openTCP); err == nil {
		listener.Close()
		t.Error("a listener with no socket file at the path")
	}
	path := filepath.Join(dir, "read.sock")
	openUnix := func(path string, _ os.FileMode) (net.Listener, error) {
		listener, err := bindSocket(path, 0o660)
		if err != nil {
			t.Fatal(err)
		}
		return listener, nil
	}
	if listener, err := socketListener(path, 0o600, openUnix); err == nil {
		listener.Close()
		t.Fatal("the listener accepted a socket with mode 0660 when the configured mode is 0600")
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("the socket file must be absent after the listener closes: %v", err)
	}
}
