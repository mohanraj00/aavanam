package pg

import (
	"context"
	"os"
	"reflect"
	"strconv"
	"testing"

	"github.com/mohanraj00/aavanam/backend/core/canon"
	"github.com/mohanraj00/aavanam/backend/core/check"
	"github.com/mohanraj00/aavanam/backend/core/envelope"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/ops"
	"github.com/mohanraj00/aavanam/backend/core/read"
	"github.com/mohanraj00/aavanam/backend/core/storagetest"
	"github.com/mohanraj00/aavanam/backend/core/text"
	"github.com/mohanraj00/aavanam/backend/core/write"
)

// toV040 changes the database into the state that version 0.4.0 leaves: schema version 2, the content, each
// read context with its member list, and no derived table of the read path.
const toV040 = `
CREATE TABLE context_members (
    context bytea NOT NULL,
    ref bytea NOT NULL,
    PRIMARY KEY (context, ref)
);
INSERT INTO context_members (context, ref)
    SELECT DISTINCT x.id, c.member FROM contexts x CROSS JOIN LATERAL unnest(x.roots) AS r(root)
    JOIN closures c ON c.root = r.root;
DROP TABLE closures, text_tokens, table_heads, table_rows;
DROP INDEX objects_layers;
DROP TRIGGER inserts_only ON objects;
DROP TRIGGER inserts_only ON edges;
DROP TRIGGER inserts_only ON payloads;
DROP TRIGGER inserts_only ON candidates;
CREATE TRIGGER inserts_only BEFORE UPDATE OR DELETE ON objects FOR EACH STATEMENT EXECUTE FUNCTION aavanam_inserts_only();
CREATE TRIGGER inserts_only BEFORE UPDATE OR DELETE ON edges FOR EACH STATEMENT EXECUTE FUNCTION aavanam_inserts_only();
CREATE TRIGGER inserts_only BEFORE UPDATE OR DELETE ON payloads FOR EACH STATEMENT EXECUTE FUNCTION aavanam_inserts_only();
CREATE TRIGGER inserts_only BEFORE UPDATE OR DELETE ON candidates FOR EACH STATEMENT EXECUTE FUNCTION aavanam_inserts_only();
DROP FUNCTION aavanam_removal_only();
ALTER TABLE contexts DROP COLUMN expires, DROP COLUMN gone;
DROP TABLE removals, removal_epoch;
DELETE FROM schema_migrations WHERE version > 2;
`

func revisionOf(t *testing.T, descriptor string) string {
	t.Helper()
	value, err := jsontext.Parse([]byte(descriptor), jsontext.Strict)
	if err != nil {
		t.Fatal(err)
	}
	normal, refusal := check.Normal(value)
	if refusal != nil {
		t.Fatal(refusal)
	}
	return canon.Revision(normal)
}

func count(t *testing.T, s *Store, query string) int {
	t.Helper()
	var n int
	if err := s.pool.QueryRow(context.Background(), query).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// A store of version 0.4.0 has no closure table, no text index and no row cache. After the schema change,
// a context that the old version opened has the same members, and each read gives the same result: a
// search scans the digests that the index does not have, and then the index has them.
func TestUpgradeFromVersion040(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	registry := func() *ops.Registry {
		r := ops.NewRegistry()
		read.Register(r)
		text.Register(r)
		write.Register(r)
		return r
	}
	call := func(store *ops.Store, operation, request string) envelope.Envelope {
		return store.Call(ctx, jsontext.S(operation), []byte(request))
	}
	const profile = `"descriptor_profile":"aavanam/base/v1"`
	payload := func(data, media string) string {
		return `"payload":{"digest":"` + jsontext.Digest([]byte(data)) + `","media_type":"` + media + `","size":` +
			strconv.Itoa(len(data)) + `}`
	}
	one, two := "alpha beta gamma", "beta alpha beta"
	rows := `{"columns":["key","value"],"key":["key"],"rows":[["k0",1],["k1",2]]}`
	fields := `{"fields":["key","value"]}`
	first := `{` + profile + `,"kind":"artifact","artifact_type":"note",` + payload(one, "text/plain") + `}`
	second := `{` + profile + `,"kind":"artifact","artifact_type":"note",` + payload(two, "text/plain") + `}`
	schema := `{` + profile + `,"kind":"schema","schema_profile":"sample/schema/v1",` + payload(fields, "application/json") + `}`
	table := `{` + profile + `,"kind":"data-resource","data_profile":"aavanam/table/v1","schema":"` + revisionOf(t, schema) + `",` +
		payload(rows, "application/json") + `}`
	bindings := `{` + profile + `,"kind":"binding-set","bindings":{"a":"` + revisionOf(t, first) + `","b":"` + revisionOf(t, second) +
		`","table":"` + revisionOf(t, table) + `"}}`
	unit := `{` + profile + `,"kind":"knowledge-unit","logical_id":"u","binding_set":"` + revisionOf(t, bindings) + `"}`
	old := ops.New(ops.Writer, s, registry())
	put := storagetest.Put([]string{first, second, schema, table, bindings, unit}, one, two, rows, fields)
	if e := call(old, "put", put); e.Status != "ok" {
		t.Fatalf("put: %s", e.JSON())
	}
	opened := call(old, "open_context", `{"roots":["`+revisionOf(t, unit)+`"]}`)
	if opened.Status != "ok" {
		t.Fatalf("open_context: %s", opened.JSON())
	}
	id := `"context":"` + opened.Context.Str + `"`
	requests := [][2]string{
		{"search_text", `{` + id + `,"query":"Alpha BETA"}`},
		{"search_text", `{` + id + `,"query":"gamma"}`},
		{"search_text", `{` + id + `,"query":"delta"}`},
		{"read", `{` + id + `,"ref":"` + revisionOf(t, table) + `","selector":{"selector_profile":"aavanam/row/v1","index":1}}`},
		{"read", `{` + id + `,"ref":"` + revisionOf(t, table) + `","selector":{"selector_profile":"aavanam/row/v1","index":2}}`},
		{"query_data", `{` + id + `,"resources":["` + revisionOf(t, table) + `"],"where":{"value":2}}`},
		{"query_data", `{` + id + `,"resources":["` + revisionOf(t, table) + `"],"where":{"none":2}}`},
		{"get", `{` + id + `,"refs":["` + revisionOf(t, first) + `"]}`},
	}
	var want []string
	for _, r := range requests {
		want = append(want, string(call(old, r[0], r[1]).JSON()))
	}
	members := s.Session(ctx).Members(opened.Context.Str)
	if len(members) != 6 {
		t.Fatalf("the context has %d members", len(members))
	}

	t.Cleanup(func() { _ = Migrate(ctx, s.pool) }) // a failure of this test leaves the current schema
	if _, err := s.pool.Exec(ctx, toV040); err != nil {
		t.Fatal(err)
	}
	upgraded, err := Open(ctx, os.Getenv("AAVANAM_PG_DSN")) // the open applies the schema change
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(upgraded.Close)
	if n := count(t, upgraded, `SELECT count(*) FROM pg_tables WHERE tablename = 'context_members'`); n != 0 {
		t.Error("the member list of version 0.4.0 is there after the schema change")
	}
	if n := count(t, upgraded, `SELECT count(*) FROM text_tokens`) + count(t, upgraded, `SELECT count(*) FROM table_heads`); n != 0 {
		t.Errorf("the schema change made %d rows of the text index and the row cache", n)
	}
	r := upgraded.Session(ctx)
	// A context of the old version has no declared life (CTX-LIF-1), and the store has no removal record.
	if record, kept := r.Context(opened.Context.Str); !kept || record.Expires != 0 || len(r.Removals()) != 0 ||
		count(t, upgraded, `SELECT count(*) FROM removal_epoch`) != 1 {
		t.Errorf("the old context after the schema change: %+v, kept %v", record, kept)
	}
	if got := r.Members(opened.Context.Str); !reflect.DeepEqual(got, members) {
		t.Errorf("the members of the old context after the schema change: %v, want %v", got, members)
	}
	if !r.IsMember(opened.Context.Str, revisionOf(t, table)) || r.IsMember(opened.Context.Str, refA) {
		t.Error("the member check of the old context after the schema change")
	}
	store := ops.New(ops.Writer, upgraded, registry())
	for round := range 2 { // the first round fills the text index and the row cache, the second round reads them
		for n, request := range requests {
			if got := string(call(store, request[0], request[1]).JSON()); got != want[n] {
				t.Errorf("round %d, %s %s: %s\nwant %s", round, request[0], request[1], got, want[n])
			}
		}
		if n := count(t, upgraded, `SELECT count(*) FROM text_tokens`); n != 2 {
			t.Errorf("round %d: the text index has %d digests, not 2", round, n)
		}
		if n := count(t, upgraded, `SELECT count(*) FROM table_rows`); n != 2 {
			t.Errorf("round %d: the row cache has %d rows, not 2", round, n)
		}
	}
	again := call(store, "open_context", `{"roots":["`+revisionOf(t, unit)+`"]}`)
	if got := upgraded.Session(ctx).Members(again.Context.Str); again.Status != "ok" || !reflect.DeepEqual(got, members) {
		t.Errorf("a new context after the schema change: %s, members %v", again.JSON(), got)
	}
}
