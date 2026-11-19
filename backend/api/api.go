// Package api has the network binding of the store. Read is the network read binding `aavanam-read/v1`
// (document 22): the operations that do not write content, over HTTP. Write is the listener of a service
// for each operation of the operation table. It is not a part of the binding (NET-TRN-5).
//
// A handler gives the bytes of a request body to the core with no decode (NET-TRN-2). Each result of an
// operation has the HTTP status 200, and its status is in the envelope (NET-TRN-3). Access control is not
// in the scope of the specification: a deployment puts its access control in front of a listener.
package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mohanraj00/aavanam/backend/core/canon"
	"github.com/mohanraj00/aavanam/backend/core/envelope"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/kinds"
	"github.com/mohanraj00/aavanam/backend/core/ops"
)

// The identity of the API and its minor version (NET-VER-1).
const (
	Identity = "aavanam-read/v1"
	Minor    = 0
)

// The limits of a listener with no configured value.
const (
	DefaultMaxBodyBytes = 16 << 20
	DefaultDeadline     = 30 * time.Second
)

const (
	aboutPath = "/v1/about"
	opPrefix  = "/v1/op/"
)

// Config is the configuration of one listener.
type Config struct {
	// Store returns the store of a call.
	Store func() *ops.Store
	// MaxBodyBytes is the limit of the size of a request body (NET-LIM-1). Zero is DefaultMaxBodyBytes.
	MaxBodyBytes int64
	// Deadline is the deadline of one call (NET-LIM-2). Zero is DefaultDeadline.
	Deadline time.Duration
}

func (c Config) limits() Config {
	if c.MaxBodyBytes <= 0 {
		c.MaxBodyBytes = DefaultMaxBodyBytes
	}
	if c.Deadline <= 0 {
		c.Deadline = DefaultDeadline
	}
	return c
}

// Read returns the handler of the network read binding (NET-TRN-1 to NET-VER-1).
func Read(config Config) http.Handler { return &handler{Config: config.limits(), read: true} }

// Write returns the handler of the listener for each operation of the operation table. It has the paths
// and the responses of the read binding, but for `GET /v1/about`. A call runs to its end or to the deadline
// of its storage statements: the handler never gives `deadline_exceeded` for an update that can still
// commit (STS-ENV-4).
func Write(config Config) http.Handler { return &handler{Config: config.limits()} }

type handler struct {
	Config
	read bool
}

// refuse gives a response with no envelope (NET-TRN-4).
func refuse(w http.ResponseWriter, status int, allow string) {
	if allow != "" {
		w.Header().Set("Allow", allow)
	}
	http.Error(w, http.StatusText(status), status)
}

func send(w http.ResponseWriter, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// operationOf returns the operation name of a path `/v1/op/<operation>`: one path segment of UTF-8 text.
func operationOf(u *url.URL) (string, bool) {
	segment, found := strings.CutPrefix(u.EscapedPath(), opPrefix)
	if !found || segment == "" || strings.Contains(segment, "/") {
		return "", false
	}
	name, err := url.PathUnescape(segment)
	if err != nil || !utf8.ValidString(name) {
		return "", false
	}
	return name, true
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.read && r.URL.EscapedPath() == aboutPath {
		if r.Method != http.MethodGet {
			refuse(w, http.StatusMethodNotAllowed, http.MethodGet)
			return
		}
		send(w, h.about())
		return
	}
	name, isCall := operationOf(r.URL)
	if !isCall {
		refuse(w, http.StatusNotFound, "")
		return
	}
	if r.Method != http.MethodPost {
		refuse(w, http.StatusMethodNotAllowed, http.MethodPost)
		return
	}
	store := h.Store()
	if _, reserved := ops.Reserved[name]; reserved || !ops.Known(name) {
		// NET-SCP-2: the envelope of STS-MAL-4. The server does not check the body.
		send(w, store.Call(r.Context(), jsontext.S(name), nil).JSON())
		return
	}
	if h.read && !ops.InReadBinding(name) { // NET-SCP-3: the server does not run the operation
		refuse(w, http.StatusNotFound, "")
		return
	}
	if r.ContentLength > h.MaxBodyBytes { // NET-LIM-1
		refuse(w, http.StatusRequestEntityTooLarge, "")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, h.MaxBodyBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			refuse(w, http.StatusRequestEntityTooLarge, "")
		} else {
			refuse(w, http.StatusBadRequest, "") // the client did not send the body that it declared
		}
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.Deadline)
	defer cancel()
	if !h.read {
		send(w, store.Call(ctx, jsontext.S(name), body).JSON())
		return
	}
	// NET-LIM-2: the response has the complete result or `deadline_exceeded`, never a part of the items.
	// An operation of the binding writes no content, so a call that the deadline stopped changes none.
	done := make(chan envelope.Envelope, 1)
	go func() { done <- store.Call(ctx, jsontext.S(name), body) }()
	select {
	case result := <-done:
		send(w, result.JSON())
	case <-ctx.Done():
		if r.Context().Err() != nil {
			return // the client closed the connection
		}
		send(w, store.Stopped(name, body, "deadline_exceeded").JSON())
	}
}

// about returns the description of the API (NET-VER-1).
func (h *handler) about() []byte {
	store := h.Store()
	var operations []string
	for _, operation := range ops.Operations() {
		if ops.InReadBinding(operation) && store.Handles(operation) {
			operations = append(operations, operation)
		}
	}
	sort.Strings(operations)
	return canon.Canonical(jsontext.O(
		jsontext.M("api", jsontext.S(Identity)),
		jsontext.M("api_minor", jsontext.N(Minor)),
		jsontext.M("profile", jsontext.S(kinds.BaseProfile)),
		jsontext.M("profile_minor", jsontext.N(kinds.ProfileMinor)),
		jsontext.M("operations", jsontext.Strings(operations)),
		jsontext.M("max_body_bytes", jsontext.N(float64(h.MaxBodyBytes))),
		jsontext.M("deadline_ms", jsontext.N(float64(h.Deadline.Milliseconds()))),
	))
}

// Server returns an HTTP server for a handler of this package, with time limits for the header, the body
// and an idle connection. `deadline` is the deadline of one call.
func Server(handler http.Handler, deadline time.Duration) *http.Server {
	if deadline <= 0 {
		deadline = DefaultDeadline
	}
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       deadline + 30*time.Second,
		WriteTimeout:      2*deadline + 30*time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 16,
	}
}
