package text_test

import (
	"context"
	"reflect"
	"strconv"
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

// TokensOf gives the tokens of Tokens(Words(data)), also for a word that is too large for its cache and
// for a second call (DEV-FTS-3).
func TestTokensOf(t *testing.T) {
	long := strings.Repeat("Z", 100)
	for _, data := range []string{"", " ;", "Alpha beta ALPHA\x00b\xc3\x89 9", long + " a " + long, "alpha"} {
		for range 2 {
			if got, want := text.TokensOf([]byte(data)), text.Tokens(text.Words([]byte(data))); !reflect.DeepEqual(got, want) {
				t.Errorf("%q: %v, want %v", data, got, want)
			}
		}
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
	asked  []string   // each digest of each TextCandidates call
	writes [][]string // the digests of each update that put tokens
}

func (x *index) PutTextTokens(digest string, tokens []string) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if n := len(x.writes); n > 0 {
		x.writes[n-1] = append(x.writes[n-1], digest)
	}
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

func (x *index) TextCandidates(tokens, digests []string) (candidates, unindexed []string) {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.asked = append(x.asked, digests...)
	for _, digest := range digests {
		list, ok := x.tokens[digest]
		switch {
		case !ok:
			unindexed = append(unindexed, digest)
		case !x.none && strings.Contains(" "+strings.Join(list, " ")+" ", " "+strings.Join(tokens, " ")+" "):
			candidates = append(candidates, digest)
		}
	}
	return candidates, unindexed
}

// indexed is a storage in memory with a text index.
type indexed struct {
	mem *memstore.Store
	*index
	reads []string
}

// scan is the same storage with no text index: each search scans each text member.
type scan struct{ mem *memstore.Store }

func (s scan) Session(ctx context.Context) ops.Session {
	return struct{ ops.Session }{s.mem.Session(ctx)}
}

type session struct {
	ops.Session
	*index
	reads *[]string // each digest of each Blob call
}

func (s session) Blob(digest string) ([]byte, bool) {
	*s.reads = append(*s.reads, digest)
	return s.Session.Blob(digest)
}

type tx struct {
	ops.Tx
	*index
}

func (s *indexed) Session(ctx context.Context) ops.Session {
	return session{s.mem.Session(ctx), s.index, &s.reads}
}

func (s session) Update(fn func(t ops.Tx)) {
	s.index.mu.Lock()
	s.index.writes = append(s.index.writes, nil)
	s.index.mu.Unlock()
	s.Session.Update(func(t ops.Tx) { fn(tx{t, s.index}) })
}

// object returns the stored form of a descriptor for a write below the operations.
func object(t *testing.T, desc string, targets ...string) ops.Object {
	t.Helper()
	value, err := jsontext.Parse([]byte(desc), jsontext.Strict)
	if err != nil {
		t.Fatal(err)
	}
	normal, refusal := check.Normal(value)
	if refusal != nil {
		t.Fatal(refusal)
	}
	return ops.Object{Ref: canon.Revision(normal), Kind: check.Kind(normal), Text: canon.Canonical(normal), Targets: targets}
}

func artifactOf(digest string, size int, kind string) string {
	return `{` + profile + `,"kind":"artifact","artifact_type":"` + kind + `","payload":{"digest":"` + digest +
		`","media_type":"text/plain","size":` + strconv.Itoa(size) + `}}`
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

// A search with the index gives the same items as a full scan, also for a member with no usable bytes
// (CRD-TXT-5, BUN-IMP-12): a member whose bytes are not in the store, and a member whose bytes are in the
// store and whose deferred check fails. The index never reads the bytes of a member that it can skip.
func TestSearchWithIndexIsAFullScan(t *testing.T) {
	r := ops.NewRegistry()
	read.Register(r)
	text.Register(r)
	mem := memstore.New()
	storage := &indexed{mem: mem, index: &index{tokens: map[string][]string{}}}
	store, plain := ops.New(ops.Writer, storage, r), ops.New(ops.Writer, scan{mem}, r)

	hit, miss, absent := []byte("alpha beta gamma"), []byte("delta epsilon"), []byte("alpha beta not here")
	hitDigest, missDigest, absentDigest := jsontext.Digest(hit), jsontext.Digest(miss), jsontext.Digest(absent)
	good := object(t, artifactOf(hitDigest, len(hit), "good"))
	other := object(t, artifactOf(missDigest, len(miss), "other"))
	// The bytes of this member are in the store, but its declared size is wrong: its deferred check fails.
	wrong := object(t, artifactOf(hitDigest, len(hit)+1, "wrong-size"))
	wrongNoMatch := object(t, artifactOf(missDigest, len(miss)+1, "wrong-size"))
	// This member has a deferred record, and its check passes.
	late := object(t, artifactOf(missDigest, len(miss), "late"))
	gone := object(t, artifactOf(absentDigest, len(absent), "gone"))
	artifacts := []ops.Object{good, other, wrong, wrongNoMatch, late, gone}
	bindings := object(t, `{`+profile+`,"kind":"binding-set"}`)
	narratives, targets := "", []string{bindings.Ref}
	for i, a := range artifacts {
		if i > 0 {
			narratives += ","
		}
		narratives += `"` + a.Ref + `"`
		targets = append(targets, a.Ref)
	}
	unit := object(t, `{`+profile+`,"kind":"knowledge-unit","binding_set":"`+bindings.Ref+`","narratives":[`+narratives+`]}`, targets...)
	snapshot := object(t, `{`+profile+`,"kind":"snapshot","collection":"c","units":["`+unit.Ref+`"]}`, unit.Ref)
	storage.Session(context.Background()).Update(func(tx ops.Tx) {
		for _, o := range append(artifacts, bindings, unit, snapshot) {
			tx.PutObject(o)
		}
		tx.PutBlob(hitDigest, hit)
		tx.PutBlob(missDigest, miss)
		tx.PutDeferred(hitDigest, wrong.Ref)
		tx.PutDeferred(missDigest, wrongNoMatch.Ref)
		tx.PutDeferred(missDigest, late.Ref)
		tx.(ops.TextIndex).PutTextTokens(hitDigest, text.Tokens(text.Words(hit)))
		tx.(ops.TextIndex).PutTextTokens(missDigest, text.Tokens(text.Words(miss)))
	})
	opened := call(t, store, "open_context", `{"roots":["`+snapshot.Ref+`"]}`)
	if opened.Status != "ok" {
		t.Fatalf("open_context: %s", opened.JSON())
	}
	for query, items := range map[string]int{"Alpha BETA": 4, "epsilon": 5, "no such word": 3} {
		request := `{"context":"` + opened.Context.Str + `","query":"` + query + `"}`
		storage.reads, storage.asked = nil, nil
		got, want := call(t, store, "search_text", request), call(t, plain, "search_text", request)
		if string(got.JSON()) != string(want.JSON()) || len(got.Items) != items {
			t.Errorf("%q with the index: %s\nwith a full scan: %s", query, got.JSON(), want.JSON())
		}
		for _, refused := range []string{wrong.Ref, wrongNoMatch.Ref, gone.Ref} {
			if !strings.Contains(string(got.JSON()), `"refs":["`+refused+`"]`) {
				t.Errorf("%q: the member %s has no `payload_missing` item: %s", query, refused, got.JSON())
			}
		}
		if len(storage.asked) != 3 {
			t.Errorf("%q: the index got %d digests, not the 3 digests of the members", query, len(storage.asked))
		}
		// The search reads the bytes of `good` and `other` only if they are candidates: the index has
		// their digests, and they have no deferred record. Each member with a deferred record gives two
		// reads (the bytes, and the deferred check), and `gone` gives one.
		if query == "no such word" && len(storage.reads) != 7 {
			t.Errorf("the search read the bytes of a member that the index skips: %d reads", len(storage.reads))
		}
	}
}

// A store that got its text payloads before it had the index has no tokens for them (a store of version
// 0.4.0). A search scans each such digest, gives the same items, and gives the tokens to the index.
func TestSearchFillsTheIndex(t *testing.T) {
	r := ops.NewRegistry()
	read.Register(r)
	text.Register(r)
	mem := memstore.New()
	storage := &indexed{mem: mem, index: &index{tokens: map[string][]string{}}}
	store := ops.New(ops.Writer, storage, r)
	one, two := []byte("alpha beta"), []byte("gamma alpha")
	first := object(t, artifactOf(jsontext.Digest(one), len(one), "a"))
	second := object(t, artifactOf(jsontext.Digest(two), len(two), "b"))
	bindings := object(t, `{`+profile+`,"kind":"binding-set"}`)
	unit := object(t, `{`+profile+`,"kind":"knowledge-unit","binding_set":"`+bindings.Ref+`","narratives":["`+first.Ref+`","`+
		second.Ref+`"]}`, bindings.Ref, first.Ref, second.Ref)
	storage.Session(context.Background()).Update(func(tx ops.Tx) { // no call of the indexer, as in the old store
		for _, o := range []ops.Object{first, second, bindings, unit} {
			tx.PutObject(o)
		}
		tx.PutBlob(jsontext.Digest(one), one)
		tx.PutBlob(jsontext.Digest(two), two)
	})
	opened := call(t, store, "open_context", `{"roots":["`+unit.Ref+`"]}`)
	request := `{"context":"` + opened.Context.Str + `","query":"alpha"}`
	before := call(t, store, "search_text", request)
	if before.Status != "ok" || len(before.Items) != 2 {
		t.Fatalf("search_text with no tokens: %s", before.JSON())
	}
	for _, data := range [][]byte{one, two} {
		if got, want := storage.tokens[jsontext.Digest(data)], text.Tokens(text.Words(data)); !reflect.DeepEqual(got, want) {
			t.Errorf("the tokens of %q after the search: %v, want %v", data, got, want)
		}
	}
	storage.reads = nil
	after := call(t, store, "search_text", `{"context":"`+opened.Context.Str+`","query":"beta"}`)
	if after.Status != "ok" || len(after.Items) != 1 || len(storage.reads) != 1 {
		t.Errorf("the second search read %d payloads: %s", len(storage.reads), after.JSON())
	}
	if again := call(t, store, "search_text", request); string(again.JSON()) != string(before.JSON()) {
		t.Errorf("the search with the index: %s\nbefore: %s", again.JSON(), before.JSON())
	}
}

// TestSearchFillsTheIndexInGroups checks that a search with no tokens writes the tokens of each group of
// payloads before it reads the next group. Thus the search does not keep the tokens of all payloads.
func TestSearchFillsTheIndexInGroups(t *testing.T) {
	r := ops.NewRegistry()
	read.Register(r)
	text.Register(r)
	mem := memstore.New()
	storage := &indexed{mem: mem, index: &index{tokens: map[string][]string{}}}
	store := ops.New(ops.Writer, storage, r)
	// Three payloads of 5 MiB: two of them are more than one group of 8 MiB.
	word := strings.Repeat("w", 1023)
	var payloads [][]byte
	var objects []ops.Object
	narratives := ""
	for _, name := range []string{"alpha", "beta", "gamma"} {
		data := []byte(name + " " + strings.Repeat(word+" ", 5*1024))
		payloads = append(payloads, data)
		o := object(t, artifactOf(jsontext.Digest(data), len(data), name))
		objects = append(objects, o)
		narratives += `,"` + o.Ref + `"`
	}
	bindings := object(t, `{`+profile+`,"kind":"binding-set"}`)
	refs := []string{bindings.Ref}
	for _, o := range objects {
		refs = append(refs, o.Ref)
	}
	unit := object(t, `{`+profile+`,"kind":"knowledge-unit","binding_set":"`+bindings.Ref+`","narratives":[`+narratives[1:]+`]}`, refs...)
	storage.Session(context.Background()).Update(func(tx ops.Tx) { // no call of the indexer, as in the old store
		for _, o := range append(objects, bindings, unit) {
			tx.PutObject(o)
		}
		for _, data := range payloads {
			tx.PutBlob(jsontext.Digest(data), data)
		}
	})
	opened := call(t, store, "open_context", `{"roots":["`+unit.Ref+`"]}`)
	storage.writes = nil
	found := call(t, store, "search_text", `{"context":"`+opened.Context.Str+`","query":"beta"}`)
	if found.Status != "ok" || len(found.Items) != 1 {
		t.Fatalf("search_text with no tokens: %d items, status %s", len(found.Items), found.Status)
	}
	if len(storage.writes) != 3 {
		t.Fatalf("the search wrote the tokens in %d updates, want one for each of the 3 groups", len(storage.writes))
	}
	for n, digests := range storage.writes {
		if len(digests) != 1 {
			t.Errorf("update %d put the tokens of %d payloads, want 1", n, len(digests))
		}
	}
	if len(storage.tokens) != 3 {
		t.Errorf("the index has the tokens of %d payloads, want 3", len(storage.tokens))
	}
}
