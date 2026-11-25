// Package device is the device store of document 21: a device store and its local output store in two
// SQLite files, with the FTS5 index of `search_text` (layout `aavanam/device-sqlite/v1`). Each store is an
// ops.Storage of the Go core.
package device

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"modernc.org/sqlite"

	"github.com/mohanraj00/aavanam/backend/core/canon"
	"github.com/mohanraj00/aavanam/backend/core/check"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/ops"
)

// Layout is the identity of the layout profile (DEV-PRF-1).
const Layout = "aavanam/device-sqlite/v1"

// The files of a device store and their roles (DEV-LAY-1).
const (
	DeviceFile = "device.sqlite"
	OutputFile = "output.sqlite"
)

// tables has the tables of both files (DEV-LAY-3, DEV-OUT-1). The indexes are not tables.
var tables = []string{
	`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
	`CREATE TABLE objects (ref BLOB PRIMARY KEY, kind BLOB NOT NULL, logical_id BLOB, collection BLOB,
		canonical BLOB NOT NULL) WITHOUT ROWID`,
	`CREATE INDEX objects_snapshot ON objects (collection) WHERE kind = CAST('snapshot' AS BLOB)`,
	`CREATE TABLE edges (src BLOB NOT NULL, dst BLOB NOT NULL, field TEXT NOT NULL, PRIMARY KEY (src, dst, field))
		WITHOUT ROWID`,
	`CREATE INDEX edges_dst ON edges (dst, src)`,
	`CREATE TABLE payloads (digest BLOB PRIMARY KEY, size INTEGER NOT NULL, data BLOB NOT NULL) WITHOUT ROWID`,
	`CREATE TABLE deferred (digest BLOB NOT NULL, ref BLOB NOT NULL, PRIMARY KEY (digest, ref)) WITHOUT ROWID`,
	`CREATE INDEX deferred_ref ON deferred (ref)`,
	`CREATE TABLE contexts (id TEXT PRIMARY KEY, roots BLOB NOT NULL, capabilities BLOB NOT NULL)`,
	`CREATE TABLE closures (root BLOB NOT NULL, member BLOB NOT NULL, PRIMARY KEY (root, member)) WITHOUT ROWID`,
	`CREATE TABLE table_rows (digest BLOB NOT NULL, idx INTEGER NOT NULL, row BLOB NOT NULL, PRIMARY KEY (digest, idx))
		WITHOUT ROWID`,
}

// deviceTables has the tables of the device file only (DEV-LAY-3, DEV-FTS-1).
var deviceTables = []string{
	`CREATE TABLE imports (seq INTEGER PRIMARY KEY, roots BLOB NOT NULL)`,
	`CREATE TABLE text_docs (id INTEGER PRIMARY KEY, digest BLOB NOT NULL UNIQUE)`,
	`CREATE VIRTUAL TABLE text_index USING fts5(tokens, tokenize='ascii', detail='full', content='')`,
}

// Create makes the two files of a device store in a directory, with their tables and their `meta` rows
// (DEV-LAY-1, DEV-PRF-2). A file that is there already is an error.
func Create(dir string) error {
	for name, role := range map[string]string{DeviceFile: "device", OutputFile: "output"} {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s is there already", path)
		}
		db, err := sql.Open("sqlite", path)
		if err != nil {
			return err
		}
		statements := append([]string{`PRAGMA journal_mode = WAL`}, tables...)
		if role == "device" {
			statements = append(statements, deviceTables...)
		}
		for _, s := range statements {
			if _, err = db.Exec(s); err != nil {
				break
			}
		}
		if err == nil {
			_, err = db.Exec(`INSERT INTO meta (key, value) VALUES ('layout', ?), ('role', ?)`, Layout, role)
		}
		if closeErr := db.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

// Store is one store of the layout: the device store, or its local output store.
type Store struct {
	db      *sql.DB
	role    string
	schemas []string // the schemas that a read uses: "main", and "device" for the local output store
	// heads has the declared columns and the number of rows of each table that this open of the store
	// parsed (ops.RowCache). The layout has no column for them (DEV-LAY-3), so a new open parses the
	// payload of a table one time, and reads its rows from `table_rows` after that.
	heads sync.Map // digest, then tableHead
}

// tableHead is the head of one table of the row cache.
type tableHead struct {
	columns []string
	count   int
}

// Open opens the device store and the local output store of a directory. It checks the `meta` rows
// and the declared type of `objects.kind` in each file (DEV-PRF-2, DEV-LAY-2 rule 2).
func Open(dir string) (device, output *Store, err error) {
	// The attachment is a file URI. A relative path in it would give a URI authority, so the path is absolute.
	if dir, err = filepath.Abs(dir); err != nil {
		return nil, nil, err
	}
	devicePath, outputPath := filepath.Join(dir, DeviceFile), filepath.Join(dir, OutputFile)
	options := "?_pragma=busy_timeout(10000)&_txlock=immediate" // DEV-UPD-1: BEGIN IMMEDIATE
	device = &Store{db: sql.OpenDB(connector{dsn: devicePath + options}), role: "device", schemas: []string{"main"}}
	// DEV-OUT-2: the output file reads the device file through a read-only attachment.
	attach := (&url.URL{Scheme: "file", Path: devicePath, RawQuery: "mode=ro"}).String()
	output = &Store{db: sql.OpenDB(connector{dsn: outputPath + options, attach: attach}), role: "output",
		schemas: []string{"main", "device"}}
	for _, s := range []*Store{device, output} {
		if err = s.checkLayout(); err != nil {
			device.Close()
			output.Close()
			return nil, nil, err
		}
	}
	return device, output, nil
}

// Close closes the connections of the store.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) checkLayout() error {
	rows := map[string]string{}
	found, err := s.db.Query(`SELECT key, value FROM main.meta`)
	if err != nil {
		return fmt.Errorf("the %s file is not a store of %s: %w", s.role, Layout, err)
	}
	defer found.Close()
	for found.Next() {
		var key, value string
		if err := found.Scan(&key, &value); err != nil {
			return err
		}
		rows[key] = value
	}
	if err := found.Err(); err != nil {
		return err
	}
	if rows["layout"] != Layout || rows["role"] != s.role {
		return fmt.Errorf("the %s file is not a store of %s (DEV-PRF-2)", s.role, Layout)
	}
	var kindType string
	if err := s.db.QueryRow(`SELECT type FROM pragma_table_info('objects', 'main') WHERE name = 'kind'`).Scan(&kindType); err != nil {
		return fmt.Errorf("the %s file is not a store of %s: cannot read the declared type of objects.kind (DEV-LAY-2 rule 2): %w",
			s.role, Layout, err)
	}
	if !strings.EqualFold(kindType, "BLOB") {
		return fmt.Errorf("the %s file is not a store of %s: objects.kind has type %q, want BLOB (DEV-LAY-2 rule 2)",
			s.role, Layout, kindType)
	}
	return nil
}

// connector opens connections to one file. A connection of the output file attaches the device file.
type connector struct{ dsn, attach string }

func (c connector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Driver().Open(c.dsn)
	if err != nil || c.attach == "" {
		return conn, err
	}
	args := []driver.NamedValue{{Ordinal: 1, Value: c.attach}}
	if _, err := conn.(driver.ExecerContext).ExecContext(ctx, `ATTACH DATABASE ? AS device`, args); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func (connector) Driver() driver.Driver { return &sqlite.Driver{} }

// key returns the 32 bytes of a reference or a digest (DEV-LAY-2 rule 1).
func key(ref string) []byte {
	out, err := hex.DecodeString(strings.TrimPrefix(ref, "sha256:"))
	if err != nil || len(out) != 32 {
		ops.Raise(fmt.Errorf("%q is not a reference", ref))
	}
	return out
}

// refOf returns the reference of 32 bytes.
func refOf(b []byte) string { return "sha256:" + hex.EncodeToString(b) }

// list returns the RFC 8785 bytes of a list of strings (DEV-LAY-2 rule 4).
func list(values []string) []byte { return canon.Canonical(jsontext.Strings(values)) }

// listOf returns the strings of a JSON list of the store.
func listOf(data []byte) []string {
	v, err := jsontext.Parse(data, jsontext.Strict)
	if err != nil {
		ops.Raise(errors.New("a JSON list of the store is not JSON"))
	}
	return ops.Strings(v)
}

// querier is a connection pool or a transaction.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// view reads one store. The reads of a session use the pool, and the reads of an update use its transaction.
type view struct {
	s   *Store
	ctx context.Context
	q   querier
	// pending has the table heads of an update. The store keeps them after the commit.
	pending map[string]tableHead
}

// in returns the query for one schema: `{s}` is the schema.
func in(query, schema string) string { return strings.ReplaceAll(query, "{s}", schema) }

// one reads one value from the first schema that has it. It tells if a schema has it.
func (v view) one(query string, dst any, args ...any) bool {
	for _, schema := range v.s.schemas {
		err := v.q.QueryRowContext(v.ctx, in(query, schema), args...).Scan(dst)
		if err == nil {
			return true
		}
		if !errors.Is(err, sql.ErrNoRows) {
			ops.Raise(err)
		}
	}
	return false
}

// keys returns the references of a query in each schema, in ascending order, with no duplicate.
func (v view) keys(query string, args ...any) []string {
	parts := make([]string, len(v.s.schemas))
	for n, schema := range v.s.schemas {
		parts[n] = in(query, schema)
	}
	rows, err := v.q.QueryContext(v.ctx, `SELECT k FROM (`+strings.Join(parts, " UNION ")+`) ORDER BY k`, args...)
	if err != nil {
		ops.Raise(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			ops.Raise(err)
		}
		out = append(out, refOf(b))
	}
	if err := rows.Err(); err != nil {
		ops.Raise(err)
	}
	return out
}

func (v view) Object(ref string) (string, []byte, bool) {
	var row struct {
		kind []byte
		text []byte
	}
	for _, schema := range v.s.schemas {
		err := v.q.QueryRowContext(v.ctx, in(`SELECT kind, canonical FROM {s}.objects WHERE ref = ?`, schema), key(ref)).
			Scan(&row.kind, &row.text)
		if err == nil {
			return string(row.kind), row.text, true
		}
		if !errors.Is(err, sql.ErrNoRows) {
			ops.Raise(err)
		}
	}
	return "", nil, false
}

func (v view) Targets(ref string) []string {
	return v.keys(`SELECT dst AS k FROM {s}.edges WHERE src = ?1`, key(ref))
}

func (v view) Sources(ref string) []string {
	return v.keys(`SELECT src AS k FROM {s}.edges WHERE dst = ?1`, key(ref))
}

func (v view) Blob(digest string) ([]byte, bool) {
	var data []byte
	if !v.one(`SELECT data FROM {s}.payloads WHERE digest = ?`, &data, key(digest)) {
		return nil, false
	}
	if data == nil {
		data = []byte{}
	}
	return data, true
}

func (v view) HasDeferred(ref string) bool {
	var n int
	return v.one(`SELECT 1 FROM {s}.deferred WHERE ref = ? LIMIT 1`, &n, key(ref))
}

func (v view) DeferredRefs(digest string) []string {
	return v.keys(`SELECT ref AS k FROM {s}.deferred WHERE digest = ?1`, key(digest))
}

// HasSnapshot reads only the file of the store, as a store in memory does.
func (v view) HasSnapshot(collection string) bool {
	var n int
	err := v.q.QueryRowContext(v.ctx, `SELECT 1 FROM main.objects
		WHERE kind = CAST('snapshot' AS BLOB) AND collection = ? LIMIT 1`,
		[]byte(collection)).Scan(&n)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		ops.Raise(err)
	}
	return err == nil
}

// HasLayer tells if the file of the store has a layer with the base collection (LYR-DEF-5). The table
// `objects` has no column for the base of a layer (DEV-LAY-3), so the read uses the descriptors. Only a
// writer store has sections (PUB-SEC-1), so no operation of a device store makes this read.
func (v view) HasLayer(collection string) bool {
	rows, err := v.q.QueryContext(v.ctx, `SELECT canonical FROM main.objects WHERE kind = CAST('layer' AS BLOB)`)
	if err != nil {
		ops.Raise(err)
	}
	defer rows.Close()
	for rows.Next() {
		var text []byte
		if err := rows.Scan(&text); err != nil {
			ops.Raise(err)
		}
		desc, formError := jsontext.Parse(text, jsontext.Strict)
		if formError != nil {
			ops.Raise(errors.New("a stored descriptor is not JSON"))
		}
		if base, ok := check.CollectionOf(desc); ok && base == collection {
			return true
		}
	}
	if err := rows.Err(); err != nil {
		ops.Raise(err)
	}
	return false
}

// A device store and its local output store have no catalog, no section, no gate and no producer
// section (BUN-IMP-6, PRD-SEC-1).

func (v view) Heads([]string) map[string]string             { return map[string]string{} }
func (v view) Section(string) (string, bool)                { return "", false }
func (v view) Gated(string) bool                            { return false }
func (v view) Candidate(string) (ops.Candidate, bool)       { return ops.Candidate{}, false }
func (v view) Published(string) bool                        { return false }
func (v view) Publications(string) []ops.Publication        { return nil }
func (v view) ProducerObject(string, string) ([]byte, bool) { return nil, false }
func (v view) ProducerRemoved(string, string) bool          { return false }
func (v view) ProducerRemovals(string) []ops.Removal        { return nil }
func (v view) ProducerEntries(string, string) []ops.Entry   { return nil }

func (v view) Context(id string) (ops.ContextRecord, bool) {
	var roots, capabilities []byte
	err := v.q.QueryRowContext(v.ctx, `SELECT roots, capabilities FROM main.contexts WHERE id = ?`, id).
		Scan(&roots, &capabilities)
	if errors.Is(err, sql.ErrNoRows) {
		return ops.ContextRecord{}, false
	}
	if err != nil {
		ops.Raise(err)
	}
	return ops.ContextRecord{Roots: listOf(roots), Capabilities: listOf(capabilities)}, true
}

// members is the query of the content of a read context: the closures of its roots (CTX-OPN-2, KND-DEP-3).
const members = `SELECT DISTINCT c.member FROM main.contexts x, json_each(x.roots) r
	JOIN main.closures c ON c.root = unhex(substr(r.value, 8)) WHERE x.id = ?`

func (v view) IsMember(context, ref string) bool {
	var n int
	err := v.q.QueryRowContext(v.ctx, `SELECT 1 FROM (`+members+`) WHERE member = ? LIMIT 1`, context, key(ref)).Scan(&n)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		ops.Raise(err)
	}
	return err == nil
}

func (v view) Members(context string) []string {
	rows, err := v.q.QueryContext(v.ctx, members+` ORDER BY c.member`, context)
	if err != nil {
		ops.Raise(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			ops.Raise(err)
		}
		out = append(out, refOf(b))
	}
	if err := rows.Err(); err != nil {
		ops.Raise(err)
	}
	return out
}

// hexList returns the JSON list of the hexadecimal digits of each reference, for `json_each`.
func hexList(refs []string) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, ref := range refs {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('"')
		b.WriteString(hex.EncodeToString(key(ref)))
		b.WriteByte('"')
	}
	b.WriteByte(']')
	return b.String()
}

// Closure returns the closure of a root (ops.ClosureIndex): the rows of `closures` in each file, or, for a
// root with no row, a walk of the references (KND-DEP-3).
func (v view) Closure(root string) []string {
	if out := v.keys(`SELECT member AS k FROM {s}.closures WHERE root = ?1`, key(root)); len(out) > 0 {
		return out
	}
	return ops.Sorted(ops.ClosureIn(v, []string{root}))
}

// pairs returns the second column of a query for each key of a list, from the first schema that has the
// key. The query has `{s}` for the schema and one argument: the JSON list of the keys.
func (v view) pairs(query string, refs []string) map[string][]byte {
	out := make(map[string][]byte, len(refs))
	if len(refs) == 0 {
		return out
	}
	list := hexList(refs)
	for _, schema := range v.s.schemas {
		rows, err := v.q.QueryContext(v.ctx, in(query, schema), list)
		if err != nil {
			ops.Raise(err)
		}
		for rows.Next() {
			var ref, value []byte
			if err := rows.Scan(&ref, &value); err != nil {
				rows.Close()
				ops.Raise(err)
			}
			if value == nil {
				value = []byte{}
			}
			if _, has := out[refOf(ref)]; !has {
				out[refOf(ref)] = value
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			ops.Raise(err)
		}
	}
	return out
}

// Objects returns the canonical text of each descriptor of a list (ops.BatchReader).
func (v view) Objects(refs []string) map[string][]byte {
	return v.pairs(`SELECT ref, canonical FROM {s}.objects WHERE ref IN (SELECT unhex(value) FROM json_each(?))`, refs)
}

// Blobs returns the bytes of each digest of a list that the store has (ops.BatchReader).
func (v view) Blobs(digests []string) map[string][]byte {
	return v.pairs(`SELECT digest, data FROM {s}.payloads WHERE digest IN (SELECT unhex(value) FROM json_each(?))`, digests)
}

// Deferred returns each descriptor of a list that a deferred record names (ops.BatchReader).
func (v view) Deferred(refs []string) map[string]bool {
	out := map[string]bool{}
	if len(refs) == 0 {
		return out
	}
	for _, ref := range v.keys(`SELECT ref AS k FROM {s}.deferred WHERE ref IN (SELECT unhex(value) FROM json_each(?1))`, hexList(refs)) {
		out[ref] = true
	}
	return out
}

// TextIndexed tells if the device file has the tokens of a digest (DEV-FTS-2, DEV-OUT-3).
func (v view) TextIndexed(digest string) bool {
	var n int
	return v.textOne(`SELECT 1 FROM {d}.text_docs WHERE digest = ?`, &n, key(digest))
}

// TextCandidates reads the index for the digests of a list (ops.TextIndex). A candidate is an indexed
// digest whose document has the tokens as one phrase (DEV-FTS-4). A digest with no row in `text_docs` is
// not in the index (DEV-OUT-3). The FTS5 query has only the tokens, and no text of the request.
func (v view) TextCandidates(tokens, digests []string) (candidates, unindexed []string) {
	if len(tokens) == 0 || len(digests) == 0 {
		return nil, digests
	}
	query := strings.ReplaceAll(`SELECT digest, id IN (SELECT rowid FROM {d}.text_index WHERE text_index MATCH ?1)
		FROM {d}.text_docs WHERE digest IN (SELECT unhex(value) FROM json_each(?2))`, "{d}", v.deviceSchema())
	rows, err := v.q.QueryContext(v.ctx, query, `"`+strings.Join(tokens, " ")+`"`, hexList(digests))
	if err != nil {
		ops.Raise(err)
	}
	defer rows.Close()
	indexed := make(map[string]bool, len(digests))
	for rows.Next() {
		var b []byte
		var possible bool
		if err := rows.Scan(&b, &possible); err != nil {
			ops.Raise(err)
		}
		indexed[refOf(b)] = true
		if possible {
			candidates = append(candidates, refOf(b))
		}
	}
	if err := rows.Err(); err != nil {
		ops.Raise(err)
	}
	for _, digest := range digests {
		if !indexed[digest] {
			unindexed = append(unindexed, digest)
		}
	}
	return candidates, unindexed
}

// PutTextTokens adds the document of a digest to the text index of the device file (DEV-FTS-2, DEV-FTS-3).
// The output file has no text index (DEV-OUT-1), so on the local output store it has no effect. A search
// on the local output store then scans each digest that the device file does not index (DEV-OUT-3).
func (v view) PutTextTokens(digest string, tokens []string) {
	if v.s.role != "device" || v.TextIndexed(digest) {
		return
	}
	result, err := v.q.ExecContext(v.ctx, `INSERT INTO main.text_docs (digest) VALUES (?)`, key(digest))
	if err != nil {
		ops.Raise(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		ops.Raise(err)
	}
	if _, err := v.q.ExecContext(v.ctx, `INSERT INTO main.text_index (rowid, tokens) VALUES (?, ?)`, id,
		strings.Join(tokens, " ")); err != nil {
		ops.Raise(err)
	}
}

// The row cache (ops.RowCache). The rows of a table are in `table_rows` of the file of the store
// (DEV-LAY-3, DEV-OUT-1), from the first use of the table (DEV-LAY-4). The declared columns are in the
// memory of this open of the store. A value of the cache always comes with the payload bytes in the store.

// TableHead returns the head of a table that this open of the store parsed.
func (v view) TableHead(digest string) ([]string, int, bool) {
	if head, ok := v.pending[digest]; ok {
		return head.columns, head.count, true
	}
	if head, ok := v.s.heads.Load(digest); ok {
		return head.(tableHead).columns, head.(tableHead).count, true
	}
	return nil, 0, false
}

// PutTable adds the rows of a table, if the file does not have them, and keeps the head after the commit.
// A read with no update has no effect.
func (v view) PutTable(digest string, columns []string, rows [][]byte) {
	if _, _, ok := v.TableHead(digest); ok || v.pending == nil {
		return
	}
	var n int
	err := v.q.QueryRowContext(v.ctx, `SELECT 1 FROM main.table_rows WHERE digest = ? LIMIT 1`, key(digest)).Scan(&n)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		ops.Raise(err)
	}
	if err != nil { // the file has no row of the table
		for idx, row := range rows {
			if _, err := v.q.ExecContext(v.ctx, `INSERT INTO main.table_rows (digest, idx, row) VALUES (?, ?, ?)`,
				key(digest), idx, row); err != nil {
				ops.Raise(err)
			}
		}
	}
	v.pending[digest] = tableHead{append([]string(nil), columns...), len(rows)}
}

func (v view) TableRow(digest string, index int) ([]byte, bool) {
	var row []byte
	err := v.q.QueryRowContext(v.ctx, `SELECT row FROM main.table_rows WHERE digest = ? AND idx = ?`, key(digest), index).Scan(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false
	}
	if err != nil {
		ops.Raise(err)
	}
	return row, true
}

func (v view) TableRows(digest string) [][]byte {
	rows, err := v.q.QueryContext(v.ctx, `SELECT row FROM main.table_rows WHERE digest = ? ORDER BY idx`, key(digest))
	if err != nil {
		ops.Raise(err)
	}
	defer rows.Close()
	var out [][]byte
	for rows.Next() {
		var row []byte
		if err := rows.Scan(&row); err != nil {
			ops.Raise(err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		ops.Raise(err)
	}
	return out
}

// deviceSchema returns the schema of the device file in a connection of the store.
func (v view) deviceSchema() string { return v.s.schemas[len(v.s.schemas)-1] }

func (v view) textOne(query string, dst any, args ...any) bool {
	err := v.q.QueryRowContext(v.ctx, strings.ReplaceAll(query, "{d}", v.deviceSchema()), args...).Scan(dst)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		ops.Raise(err)
	}
	return err == nil
}

// Session returns the store for one call. The context gives the deadline of each query.
func (s *Store) Session(ctx context.Context) ops.Session {
	return session{view{s: s, ctx: ctx, q: s.db}}
}

type session struct{ view }

// Update runs fn in one transaction. The transaction of the device file is BEGIN IMMEDIATE (DEV-UPD-1).
// If fn panics, the transaction rolls back and the panic continues (BUN-UPD-6).
func (v session) Update(fn func(tx ops.Tx)) {
	t, err := v.s.db.BeginTx(v.ctx, nil)
	if err != nil {
		ops.Raise(err)
	}
	done := false
	defer func() {
		if !done {
			t.Rollback()
		}
	}()
	work := view{s: v.s, ctx: v.ctx, q: t, pending: map[string]tableHead{}}
	if v.s.role == "device" {
		fn(deviceTx{tx{work}})
	} else {
		fn(tx{work})
	}
	if err := t.Commit(); err != nil {
		ops.Raise(err)
	}
	done = true
	for digest, head := range work.pending {
		v.s.heads.Store(digest, head)
	}
}

// tx is one update of a store.
type tx struct{ view }

// deviceTx is one update of the device store. It also keeps the import record.
type deviceTx struct{ tx }

func (t tx) exec(query string, args ...any) {
	if _, err := t.q.ExecContext(t.ctx, query, args...); err != nil {
		ops.Raise(err)
	}
}

// LockCollections and LockDigests have no effect: the transaction has the write lock of the file.
func (t tx) LockCollections([]string) {}
func (t tx) LockDigests([]string)     {}

// PutObject adds a descriptor and its references (DEV-UPD-1 rules 1, KND-DEP-2). A descriptor that the
// store has, or that the device store of the local output store has, does not change (IDN-CAN-3, BUN-IMP-8).
func (t tx) PutObject(o ops.Object) {
	if _, _, ok := t.Object(o.Ref); ok {
		return
	}
	desc, err := jsontext.Parse(o.Text, jsontext.Strict)
	if err != nil {
		ops.Raise(fmt.Errorf("the descriptor %s is not JSON", o.Ref))
	}
	var logicalID, collection any
	if v, ok := desc.Get("logical_id"); ok {
		logicalID = []byte(v.Str) // DEV-LAY-2 rule 2
	}
	if v, ok := desc.Get("collection"); ok && v.Kind == jsontext.String {
		collection = []byte(v.Str)
	}
	ref := key(o.Ref)
	t.exec(`INSERT INTO main.objects (ref, kind, logical_id, collection, canonical) VALUES (?, ?, ?, ?, ?)`,
		ref, []byte(o.Kind), logicalID, collection, o.Text) // DEV-LAY-2 rule 2
	for _, r := range check.References(desc) {
		t.exec(`INSERT OR IGNORE INTO main.edges (src, dst, field) VALUES (?, ?, ?)`, ref, key(r.Ref), r.Field)
	}
}

// PutBlob adds the bytes of a digest one time (IDN-DIG-2).
func (t tx) PutBlob(digest string, data []byte) {
	if _, ok := t.Blob(digest); ok {
		return
	}
	if data == nil {
		data = []byte{}
	}
	t.exec(`INSERT INTO main.payloads (digest, size, data) VALUES (?, ?, ?)`, key(digest), len(data), data)
}

func (t tx) ClearDeferred(digest string) {
	t.exec(`DELETE FROM main.deferred WHERE digest = ?`, key(digest))
}

func (t tx) PutDeferred(digest, ref string) {
	t.exec(`INSERT OR IGNORE INTO main.deferred (digest, ref) VALUES (?, ?)`, key(digest), key(ref))
}

// DeclareGate has no effect. A device store has no catalog and refuses each publication (BUN-IMP-6), so a
// gated collection changes no result of the store (PUB-GAT-1). The application can declare it on each store.
func (t tx) DeclareGate(string) {}

var errNoCatalog = errors.New("a device store has no catalog, no section, no gate and no producer section")

func (t tx) DeclareSection(string, string)              { ops.Raise(errNoCatalog) }
func (t tx) PutCandidate(ops.Candidate)                 { ops.Raise(errNoCatalog) }
func (t tx) Publish(ops.Publication)                    { ops.Raise(errNoCatalog) }
func (t tx) PutProducerObject(string, string, []byte)   { ops.Raise(errNoCatalog) }
func (t tx) DeleteProducerObject(string, string)        { ops.Raise(errNoCatalog) }
func (t tx) PutProducerRemoval(string, ops.Removal)     { ops.Raise(errNoCatalog) }
func (t tx) PutProducerEntry(string, string, ops.Entry) { ops.Raise(errNoCatalog) }

// PutContext adds a read context, and the closure of each root that the file does not have (DEV-UPD-3).
// The closure of a stored root does not change, because each target of a stored descriptor is in the
// store (BUN-IMP-2). Thus a root with closure rows has all of them.
func (t tx) PutContext(id string, record ops.ContextRecord) {
	// One query gives the roots with no closure row, so a context with many roots makes one read.
	rows, err := t.q.QueryContext(t.ctx, `SELECT DISTINCT value FROM json_each(?)
		WHERE NOT EXISTS (SELECT 1 FROM main.closures WHERE root = unhex(value))`, hexList(record.Roots))
	if err != nil {
		ops.Raise(err)
	}
	var absent []string
	for rows.Next() {
		var digits string
		if err := rows.Scan(&digits); err != nil {
			rows.Close()
			ops.Raise(err)
		}
		absent = append(absent, "sha256:"+digits)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		ops.Raise(err)
	}
	for _, root := range absent {
		for member := range ops.ClosureIn(t, []string{root}) {
			t.exec(`INSERT OR IGNORE INTO main.closures (root, member) VALUES (?, ?)`, key(root), key(member))
		}
	}
	t.exec(`INSERT INTO main.contexts (id, roots, capabilities) VALUES (?, ?, ?)`, id, list(record.Roots),
		list(record.Capabilities))
}

// PutImport adds the `imports` row of an `ok` import (DEV-UPD-2).
func (t deviceTx) PutImport(roots []string) {
	t.exec(`INSERT INTO main.imports (seq, roots) VALUES ((SELECT count(*) + 1 FROM main.imports), ?)`, list(roots))
}
