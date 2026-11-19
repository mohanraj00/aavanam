package api_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mohanraj00/aavanam/backend/api"
	"github.com/mohanraj00/aavanam/backend/core/ops"
)

// PUB-RCP-2 and PUB-RCP-4: only an answer with `accepted` is a decision. Each other answer is no answer.
func TestHTTPValidator(t *testing.T) {
	var mu sync.Mutex // a handler can run after its call ended at the deadline
	var got string
	answer := func(w http.ResponseWriter) {}
	set := func(next func(http.ResponseWriter)) {
		mu.Lock()
		answer = next
		mu.Unlock()
	}
	request := func() string {
		mu.Lock()
		defer mu.Unlock()
		return got
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = r.Method + " " + r.Header.Get("Content-Type") + " " + string(body)
		current := answer
		mu.Unlock()
		current(w)
	}))
	defer server.Close()
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"accepted":true}`))
	}))
	defer elsewhere.Close()
	validate, err := api.HTTPValidator("pol", server.URL+"/decide", 200*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	text := func(body string) func(http.ResponseWriter) {
		return func(w http.ResponseWriter) { _, _ = w.Write([]byte(body)) }
	}
	for name, c := range map[string]struct {
		answer func(http.ResponseWriter)
		want   ops.Decision
	}{
		"accepted":           {text(`{"accepted":true}`), ops.Accepted},
		"not accepted":       {text(`{"accepted":false,"note":"x"}`), ops.Rejected},
		"a string":           {text(`{"accepted":"true"}`), ops.NoAnswer},
		"a number":           {text(`{"accepted":1}`), ops.NoAnswer},
		"no field":           {text(`{}`), ops.NoAnswer},
		"a duplicate key":    {text(`{"accepted":false,"accepted":true}`), ops.NoAnswer},
		"not JSON":           {text(`accepted`), ops.NoAnswer},
		"not an object":      {text(`true`), ops.NoAnswer},
		"an empty body":      {text(``), ops.NoAnswer},
		"a large body":       {text(`{"accepted":true,"note":"` + strings.Repeat("x", 1<<16) + `"}`), ops.NoAnswer},
		"a different status": {func(w http.ResponseWriter) { w.WriteHeader(500); _, _ = w.Write([]byte(`{"accepted":true}`)) }, ops.NoAnswer},
		"a status 201":       {func(w http.ResponseWriter) { w.WriteHeader(201); _, _ = w.Write([]byte(`{"accepted":true}`)) }, ops.NoAnswer},
		"a redirect":         {func(w http.ResponseWriter) { w.Header().Set("Location", elsewhere.URL); w.WriteHeader(307) }, ops.NoAnswer},
		"after the deadline": {func(w http.ResponseWriter) {
			time.Sleep(600 * time.Millisecond)
			_, _ = w.Write([]byte(`{"accepted":true}`))
		}, ops.NoAnswer},
		"a closed connection":  {func(w http.ResponseWriter) { panic(http.ErrAbortHandler) }, ops.NoAnswer},
		"accepted with spaces": {text(" {\n\"accepted\" : true } "), ops.Accepted},
	} {
		set(c.answer)
		if decision := validate(context.Background(), "sha256:c", "sha256:r", ""); decision != c.want {
			t.Errorf("%s: the decision is %d, not %d", name, decision, c.want)
		}
	}
	set(text(`{"accepted":true}`))
	validate(context.Background(), "sha256:c", "sha256:r", "sha256:a")
	if want := `POST application/json {"activation":"sha256:a","candidate":"sha256:c","policy":"pol","receipt":"sha256:r"}`; request() != want {
		t.Errorf("the request of a validator:\n got  %s\n want %s", request(), want)
	}
	validate(context.Background(), "sha256:c", "sha256:r", "")
	if !strings.Contains(request(), `"activation":null`) {
		t.Errorf("a request with no activation (PUB-RCP-2): %s", request())
	}
	server.Close()
	if decision := validate(context.Background(), "sha256:c", "sha256:r", ""); decision != ops.NoAnswer {
		t.Errorf("a validator that is not there: %d", decision)
	}
	for _, address := range []string{"", "decide", "ftp://host/x", "http://", "file:///etc/hosts", "http://user:secret@host/x", "://x"} {
		if _, err := api.HTTPValidator("pol", address, 0); err == nil {
			t.Errorf("the address %q is not an address of a validator", address)
		}
	}
}
