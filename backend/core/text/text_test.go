package text_test

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/mohanraj00/aavanam/backend/core/canon"
	"github.com/mohanraj00/aavanam/backend/core/check"
	"github.com/mohanraj00/aavanam/backend/core/envelope"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/memstore"
	"github.com/mohanraj00/aavanam/backend/core/ops"
	"github.com/mohanraj00/aavanam/backend/core/read"
	"github.com/mohanraj00/aavanam/backend/core/text"
	"github.com/mohanraj00/aavanam/backend/core/write"
)

// The word rule and the case rule apply to bytes (CRD-TXT-3).
func TestWords(t *testing.T) {
	data := []byte("Ab\x00c-D9 \xc3\xa9t\xc3\x89 x")
	var got []string
	for _, w := range text.Words(data) {
		got = append(got, string(data[w.Start:w.End])+"="+w.Folded)
	}
	want := []string{"Ab=ab", "c=c", "D9=d9", "\xc3\xa9t\xc3\x89=\xc3\xa9t\xc3\x89", "x=x"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	if text.Words(nil) != nil || text.Words([]byte(" ;\x00")) != nil {
		t.Fatal("bytes with no word byte give no word")
	}
}

// A token has 17 ASCII letters and digits, and equal folded forms give equal tokens (DEV-FTS-3).
func TestToken(t *testing.T) {
	a, b := text.Token("alpha"), text.Token(text.Words([]byte("ALPHA"))[0].Folded)
	if a != b || len(a) != 17 || a[0] != 't' || strings.Trim(a[1:], "0123456789abcdef") != "" {
		t.Fatalf("tokens %q and %q", a, b)
	}
	if text.Token("alpha") == text.Token("beta") {
		t.Fatal("two words with one token")
	}
}

func TestIsTextMedia(t *testing.T) {
	for media, want := range map[string]bool{
		"text/plain": true, " TEXT/markdown ; charset=utf-8": true, "Text/x": true,
		"text/": false, "text": false, "application/json": false, "\ttext/plain": false, "": false,
	} {
		if got := text.IsTextMedia(media); got != want {
			t.Errorf("%q: got %v", media, got)
		}
	}
}

// A query has 1 to 1024 scalar values and 1 to 32 words, and no surrogate (CRD-TXT-1).
func TestIsQuery(t *testing.T) {
	cases := map[string]bool{
		"a": true, "￿ a": true, strings.Repeat("a ", 32): true, strings.Repeat("a ", 33): false,
		"": false, " ; ": false, strings.Repeat("a", 1025): false, "a\xed\xa0\x80": false,
	}
	for query, want := range cases {
		if got := text.IsQuery(jsontext.S(query)); got != want {
			t.Errorf("%q: got %v", query, got)
		}
	}
	if text.IsQuery(jsontext.N(1)) {
		t.Error("a number is not a query")
	}
}

// Each word start can start a match, so matches can overlap (CRD-TXT-4).
func TestMatchesOverlap(t *testing.T) {
	got := text.Matches(text.Words([]byte("a a a")), text.Words([]byte("A a")))
	if want := [][2]int{{0, 3}, {2, 5}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// index is a text index in memory. If `none` is true, it gives no candidate.
type index struct {
	mu     sync.Mutex
	tokens map[string][]string
	none   bool
}

func (x *index) PutTextTokens(digest string, tokens []string) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if _, ok := x.tokens[digest]; !ok {
		x.tokens[digest] = tokens
	}
}

func (x *index) TextIndexed(digest string) bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	_, ok := x.tokens[digest]
	return ok
}

func (x *index) TextCandidates(tokens []string) []string {
	x.mu.Lock()
	defer x.mu.Unlock()
	var out []string
	for digest, list := range x.tokens {
		if !x.none && strings.Contains(" "+strings.Join(list, " ")+" ", " "+strings.Join(tokens, " ")+" ") {
			out = append(out, digest)
		}
	}
	return out
}

// indexed is a storage in memory with a text index.
type indexed struct {
	mem *memstore.Store
	*index
}

type session struct {
	ops.Session
	*index
}

type tx struct {
	ops.Tx
	*index
}

func (s *indexed) Session(ctx context.Context) ops.Session {
	return session{s.mem.Session(ctx), s.index}
}

func (s session) Update(fn func(t ops.Tx)) {
	s.Session.Update(func(t ops.Tx) { fn(tx{t, s.index}) })
}

const profile = `"descriptor_profile":"aavanam/base/v1"`

func ref(t *testing.T, desc string) string {
	t.Helper()
	value, err := jsontext.Parse([]byte(desc), jsontext.Strict)
	if err != nil {
		t.Fatal(err)
	}
	normal, refusal := check.Normal(value)
	if refusal != nil {
		t.Fatal(refusal)
	}
	return canon.Revision(normal)
}

func call(t *testing.T, store *ops.Store, operation, request string) envelope.Envelope {
	t.Helper()
	return store.Call(context.Background(), jsontext.S(operation), []byte(request))
}

// The index gets the tokens of each text payload in the update. A search scans only the indexed digests
// that the index gives, and the matches come from the bytes (CRD-TXT-3, DEV-FTS-5).
func TestSearchUsesIndex(t *testing.T) {
	r := ops.NewRegistry()
	read.Register(r)
	text.Register(r)
	write.Register(r)
	storage := &indexed{mem: memstore.New(), index: &index{tokens: map[string][]string{}}}
	store := ops.New(ops.Writer, storage, r)
	data := "Alpha beta Gamma"
	digest := jsontext.Digest([]byte(data))
	artifact := `{` + profile + `,"kind":"artifact","artifact_type":"note","payload":{"digest":"` + digest +
		`","media_type":"text/plain","size":16}}`
	bindings := `{` + profile + `,"kind":"binding-set"}`
	unit := `{` + profile + `,"kind":"knowledge-unit","binding_set":"` + ref(t, bindings) + `","narratives":["` + ref(t, artifact) + `"]}`
	snapshot := `{` + profile + `,"kind":"snapshot","collection":"c","units":["` + ref(t, unit) + `"]}`
	var texts []string
	for _, d := range []string{artifact, bindings, unit, snapshot} {
		texts = append(texts, string(canon.AppendString(nil, d)))
	}
	put := `{"descriptors":[` + strings.Join(texts, ",") + `],"payloads":{"` + digest + `":"` + ops.B64([]byte(data)) + `"}}`
	if e := call(t, store, "put", put); e.Status != "ok" {
		t.Fatalf("put: %s", e.JSON())
	}
	if got, want := storage.tokens[digest], text.Tokens(text.Words([]byte(data))); !reflect.DeepEqual(got, want) {
		t.Fatalf("tokens %v, want %v", got, want)
	}
	opened := call(t, store, "open_context", `{"roots":["`+ref(t, snapshot)+`"]}`)
	if opened.Status != "ok" {
		t.Fatalf("open_context: %s", opened.JSON())
	}
	search := `{"context":"` + opened.Context.Str + `","query":"BETA gamma"}`
	e := call(t, store, "search_text", search)
	want := `"selector":{"end":16,"selector_profile":"aavanam/bytes/v1","start":6}`
	if e.Status != "ok" || len(e.Items) != 1 || !strings.Contains(string(e.JSON()), want) {
		t.Fatalf("search_text: %s", e.JSON())
	}
	storage.none = true
	if e := call(t, store, "search_text", search); e.Status != "ok" || len(e.Items) != 0 {
		t.Fatalf("an indexed digest that the index does not give is not scanned: %s", e.JSON())
	}
}
