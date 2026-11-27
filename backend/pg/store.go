package pg

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"sort"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/ops"
)

// Store is one writer store in PostgreSQL.
type Store struct {
	pool *pgxpool.Pool
	life atomic.Int64 // the life of a read context in nanoseconds, or 0 for no end
}

// SetContextLife gives the life of a read context. After that time from its `open_context`, the store does
// not keep the context for a new call, and the call gets `context_expired`. Zero is no end. The value is
// store configuration: set it before the first call.
func (s *Store) SetContextLife(life time.Duration) { s.life.Store(int64(life)) }

// ExpireContexts removes each read context that is older than its life plus `grace`. It returns the number
// of contexts. A context is not content of the store, so the removal changes no descriptor and no bytes
// (CTX-OPN-2). The closures of its roots stay: they are a function of the content. A call that started
// before the end of the life of its context still reads the context, so `grace` must be longer than the
// deadline of one call. With no life, the method removes nothing.
func (s *Store) ExpireContexts(ctx context.Context, grace time.Duration) (int64, error) {
	life := time.Duration(s.life.Load())
	if life <= 0 {
		return 0, nil
	}
	var count int64
	err := s.pool.QueryRow(ctx, `WITH gone AS (DELETE FROM contexts WHERE opened < now() - make_interval(secs => $1) RETURNING id)
		SELECT count(*) FROM gone`, (life + grace).Seconds()).Scan(&count)
	return count, err
}

// Open connects to the database of a DSN, checks that it answers and applies the schema changes. An error
// does not have the DSN, because a DSN can have a password.
func Open(ctx context.Context, dsn string) (*Store, error) {
	if dsn == "" {
		return nil, errors.New("the PostgreSQL storage needs a DSN")
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, errors.New("the PostgreSQL configuration is not valid")
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, errors.New("PostgreSQL does not answer: " + err.Error())
	}
	if err := Migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{pool: pool}, nil
}

// Close closes the connections of the store.
func (s *Store) Close() { s.pool.Close() }

// Session returns the store for one call. The context gives the deadline of each statement.
func (s *Store) Session(ctx context.Context) ops.Session {
	return session{view{ctx, s.pool, time.Duration(s.life.Load())}, s.pool, &seen{}}
}

// seen is the removal epoch that a session read at the start of its call, if it read one.
type seen struct {
	known bool
	epoch int64
}

// raw returns the 32 bytes of a reference or a digest. A string of a different form names no row.
func raw(ref string) ([]byte, bool) {
	if !jsontext.IsDigest(ref) {
		return nil, false
	}
	out, err := hex.DecodeString(ref[len("sha256:"):])
	return out, err == nil
}

// must returns the bytes of a reference that the core made. A different form is a defect of the caller.
func must(ref string) []byte {
	out, ok := raw(ref)
	if !ok {
		ops.Raise(errors.New("a reference of a write does not have the form of a digest"))
	}
	return out
}

// optional returns the bytes of a reference, or nil for the empty string.
func optional(ref string) []byte {
	if ref == "" {
		return nil
	}
	return must(ref)
}

func name(b []byte) string {
	if b == nil {
		return ""
	}
	return "sha256:" + hex.EncodeToString(b)
}

func raws(refs []string) [][]byte {
	out := make([][]byte, len(refs))
	for i, ref := range refs {
		out[i] = must(ref)
	}
	return out
}

// querier is the part of a pool and of a transaction that the reads use.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// view reads the store through a pool or a transaction. Each failure is a storage fault (ops.Raise).
type view struct {
	ctx  context.Context
	q    querier
	life time.Duration // the life of a read context, or 0 for no end
}

// row scans one row. It returns false if the query gives no row.
func (v view) row(dest []any, sql string, args ...any) bool {
	err := v.q.QueryRow(v.ctx, sql, args...).Scan(dest...)
	if errors.Is(err, pgx.ErrNoRows) {
		return false
	}
	if err != nil {
		ops.Raise(err)
	}
	return true
}

func (v view) exists(sql string, args ...any) bool {
	var found bool
	v.row([]any{&found}, `SELECT EXISTS (`+sql+`)`, args...)
	return found
}

// each runs a query and calls scan for each row.
func (v view) each(scan func(rows pgx.Rows) error, sql string, args ...any) {
	rows, err := v.q.Query(v.ctx, sql, args...)
	if err != nil {
		ops.Raise(err)
	}
	defer rows.Close()
	for rows.Next() {
		if err := scan(rows); err != nil {
			ops.Raise(err)
		}
	}
	if err := rows.Err(); err != nil {
		ops.Raise(err)
	}
}

// refs returns the references of a query with one `bytea` column.
func (v view) refs(sql string, args ...any) []string {
	var out []string
	v.each(func(rows pgx.Rows) error {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			return err
		}
		out = append(out, name(b))
		return nil
	}, sql, args...)
	return out
}

func (v view) exec(sql string, args ...any) {
	if _, err := v.q.Exec(v.ctx, sql, args...); err != nil {
		ops.Raise(err)
	}
}

func (v view) Object(ref string) (string, []byte, bool) {
	key, ok := raw(ref)
	if !ok {
		return "", nil, false
	}
	var kind string
	var text []byte
	if !v.row([]any{&kind, &text}, `SELECT kind, canonical FROM objects WHERE ref = $1`, key) {
		return "", nil, false
	}
	return kind, text, true
}

func (v view) Targets(ref string) []string {
	key, ok := raw(ref)
	if !ok {
		return nil
	}
	return v.refs(`SELECT dst FROM edges WHERE src = $1 ORDER BY dst`, key)
}

func (v view) Sources(ref string) []string {
	key, ok := raw(ref)
	if !ok {
		return nil
	}
	return v.refs(`SELECT src FROM edges WHERE dst = $1 ORDER BY src`, key)
}

func (v view) Blob(digest string) ([]byte, bool) {
	key, ok := raw(digest)
	if !ok {
		return nil, false
	}
	data := []byte{}
	if !v.row([]any{&data}, `SELECT data FROM payloads WHERE digest = $1`, key) {
		return nil, false
	}
	return data, true
}

func (v view) HasDeferred(ref string) bool {
	key, ok := raw(ref)
	return ok && v.exists(`SELECT 1 FROM deferred WHERE ref = $1`, key)
}

func (v view) DeferredRefs(digest string) []string {
	key, ok := raw(digest)
	if !ok {
		return nil
	}
	return v.refs(`SELECT ref FROM deferred WHERE digest = $1 ORDER BY ref`, key)
}

func (v view) HasSnapshot(collection string) bool {
	return v.exists(`SELECT 1 FROM objects WHERE kind = 'snapshot' AND sha256(collection) = sha256($1) AND collection = $1`, []byte(collection))
}

// HasLayer reads the column `collection` of a layer, which has its base collection (LYR-DEF-5).
func (v view) HasLayer(collection string) bool {
	return v.exists(`SELECT 1 FROM objects WHERE kind = 'layer' AND sha256(collection) = sha256($1) AND collection = $1`, []byte(collection))
}

func (v view) Heads(collections []string) map[string]string {
	names, keys := make([][]byte, len(collections)), make([][]byte, len(collections))
	for i, c := range collections {
		sum := sha256.Sum256([]byte(c))
		names[i], keys[i] = []byte(c), sum[:]
	}
	out := map[string]string{}
	v.each(func(rows pgx.Rows) error { // one statement: one atomic read (CTX-VIW-3)
		var collection, snapshot []byte
		if err := rows.Scan(&collection, &snapshot); err != nil {
			return err
		}
		out[string(collection)] = name(snapshot)
		return nil
	}, `SELECT collection, snapshot FROM heads WHERE sha256(collection) = ANY($2::bytea[]) AND collection = ANY($1::bytea[])`, names, keys)
	return out
}

func (v view) Section(collection string) (string, bool) {
	var section string
	ok := v.row([]any{&section}, `SELECT section FROM sections WHERE sha256(collection) = sha256($1) AND collection = $1`, []byte(collection))
	return section, ok
}

func (v view) Gated(collection string) bool {
	return v.exists(`SELECT 1 FROM gates WHERE sha256(collection) = sha256($1) AND collection = $1`, []byte(collection))
}

func (v view) Candidate(id string) (ops.Candidate, bool) {
	key, ok := raw(id)
	if !ok {
		return ops.Candidate{}, false
	}
	var snapshot, collection, policy, expected []byte
	if !v.row([]any{&snapshot, &collection, &policy, &expected},
		`SELECT snapshot, collection, policy, expected_head FROM candidates WHERE id = $1`, key) {
		return ops.Candidate{}, false
	}
	return ops.Candidate{ID: id, Snapshot: name(snapshot), Collection: string(collection), Policy: string(policy),
		ExpectedHead: name(expected)}, true
}

func (v view) Published(candidate string) bool {
	key, ok := raw(candidate)
	return ok && v.exists(`SELECT 1 FROM publications WHERE candidate = $1`, key)
}

func (v view) Publications(collection string) []ops.Publication {
	var out []ops.Publication
	v.each(func(rows pgx.Rows) error {
		var candidate, receipt, activation, previous, head []byte
		if err := rows.Scan(&candidate, &receipt, &activation, &previous, &head); err != nil {
			return err
		}
		out = append(out, ops.Publication{Candidate: name(candidate), Receipt: name(receipt), Activation: name(activation),
			Collection: collection, PreviousHead: name(previous), Head: name(head)})
		return nil
	}, `SELECT candidate, receipt, activation, previous_head, head FROM publications WHERE sha256(collection) = sha256($1) AND collection = $1 ORDER BY seq`,
		[]byte(collection))
	return out
}

func (v view) Context(id string) (ops.ContextRecord, bool) {
	var roots [][]byte
	var capabilities []string
	var expires int64
	// A context after the end of the life of SetContextLife is a context that the store does not keep. The
	// core applies the declared life of the context with the store clock (CTX-LIF-2).
	if !v.row([]any{&roots, &capabilities, &expires}, `SELECT roots, capabilities, coalesce(expires, 0) FROM contexts
		WHERE id = $1 AND ($2::float8 = 0 OR opened > now() - make_interval(secs => $2::float8))`, []byte(id), v.life.Seconds()) {
		return ops.ContextRecord{}, false
	}
	record := ops.ContextRecord{Roots: make([]string, len(roots)), Capabilities: capabilities, Expires: expires}
	for i, root := range roots {
		record.Roots[i] = name(root)
	}
	return record, true
}

// The content of a read context is the closures of its roots (CTX-OPN-2, KND-DEP-3), without the removed
// objects of the column `gone` (REL-RMV-8).

func (v view) IsMember(context, ref string) bool {
	key, ok := raw(ref)
	return ok && v.exists(`SELECT 1 FROM contexts x CROSS JOIN LATERAL unnest(x.roots) AS r(root)
		JOIN closures c ON c.root = r.root WHERE x.id = $1 AND c.member = $2
		AND (x.gone IS NULL OR c.member <> ALL(x.gone))`, []byte(context), key)
}

func (v view) Members(context string) []string {
	return v.refs(`SELECT DISTINCT c.member FROM contexts x CROSS JOIN LATERAL unnest(x.roots) AS r(root)
		JOIN closures c ON c.root = r.root WHERE x.id = $1 AND (x.gone IS NULL OR c.member <> ALL(x.gone))
		ORDER BY c.member`, []byte(context))
}

// texts returns the strings of a query with one `bytea` column.
func (v view) texts(sql string, args ...any) []string {
	var out []string
	v.each(func(rows pgx.Rows) error {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			return err
		}
		out = append(out, string(b))
		return nil
	}, sql, args...)
	return out
}

func (v view) ContextsWith(ref string) []string {
	key, ok := raw(ref)
	if !ok {
		return nil
	}
	return v.texts(`SELECT x.id FROM contexts x WHERE (x.gone IS NULL OR $1 <> ALL(x.gone)) AND EXISTS (
		SELECT 1 FROM unnest(x.roots) AS r(root) JOIN closures c ON c.root = r.root WHERE c.member = $1) ORDER BY x.id`, key)
}

// OpenRoots applies the declared life of each context and the life of SetContextLife.
func (v view) OpenRoots(now int64) []string {
	return v.refs(`SELECT DISTINCT r.root FROM contexts x CROSS JOIN LATERAL unnest(x.roots) AS r(root)
		WHERE (x.expires IS NULL OR x.expires > $1) AND ($2::float8 = 0 OR x.opened > now() - make_interval(secs => $2::float8))
		ORDER BY 1`, now, v.life.Seconds())
}

func (v view) EndedContexts(now int64) []string {
	return v.texts(`SELECT id FROM contexts WHERE expires IS NOT NULL AND expires <= $1 ORDER BY id`, now)
}

func (v view) Removals() []ops.RemovalRecord {
	var out []ops.RemovalRecord
	v.each(func(rows pgx.Rows) error {
		var r ops.RemovalRecord
		var ref, reason []byte
		if err := rows.Scan(&r.Sequence, &ref, &r.Kind, &r.Time, &reason); err != nil {
			return err
		}
		r.Ref, r.Reason = name(ref), string(reason)
		out = append(out, r)
		return nil
	}, `SELECT sequence, ref, kind, time, reason FROM removals ORDER BY sequence`)
	return out
}

func (v view) HasRemoval(ref string) bool {
	key, ok := raw(ref)
	return ok && v.exists(`SELECT 1 FROM removals WHERE ref = $1`, key)
}

func (v view) RemovedRefs() []string {
	return v.refs(`SELECT DISTINCT r.ref FROM removals r WHERE NOT EXISTS (SELECT 1 FROM objects o WHERE o.ref = r.ref) ORDER BY 1`)
}

func (v view) OwnObjects() []ops.Listed {
	var out []ops.Listed
	v.each(func(rows pgx.Rows) error {
		var ref []byte
		var kind string
		if err := rows.Scan(&ref, &kind); err != nil {
			return err
		}
		out = append(out, ops.Listed{Ref: name(ref), Kind: kind})
		return nil
	}, `SELECT ref, kind FROM objects ORDER BY ref`)
	return out
}

func (v view) OwnBlobs() []string { return v.refs(`SELECT digest FROM payloads ORDER BY digest`) }

// Naming reads each descriptor: no index has the text of a descriptor. Only `remove` calls it.
func (v view) Naming(digest string) []string {
	return v.refs(`SELECT ref FROM objects WHERE position($1::bytea in canonical) > 0 ORDER BY ref`, []byte(digest))
}

func (v view) AllHeads() map[string]string {
	out := map[string]string{}
	v.each(func(rows pgx.Rows) error {
		var collection, snapshot []byte
		if err := rows.Scan(&collection, &snapshot); err != nil {
			return err
		}
		out[string(collection)] = name(snapshot)
		return nil
	}, `SELECT collection, snapshot FROM heads`)
	return out
}

func (v view) Candidates() []ops.Candidate {
	var out []ops.Candidate
	v.each(func(rows pgx.Rows) error {
		var id, snapshot, collection, policy, expected []byte
		if err := rows.Scan(&id, &snapshot, &collection, &policy, &expected); err != nil {
			return err
		}
		out = append(out, ops.Candidate{ID: name(id), Snapshot: name(snapshot), Collection: string(collection),
			Policy: string(policy), ExpectedHead: name(expected)})
		return nil
	}, `SELECT id, snapshot, collection, policy, expected_head FROM candidates ORDER BY id`)
	return out
}

// Closure returns the closure of a root in one statement (ops.ClosureIndex): the stored closure, or, for a
// root with no stored closure, a recursive read of the edges.
func (v view) Closure(root string) []string {
	key, ok := raw(root)
	if !ok {
		return []string{root}
	}
	return v.refs(`WITH RECURSIVE stored AS (SELECT member FROM closures WHERE root = $1),
		walk(ref) AS (SELECT $1::bytea WHERE NOT EXISTS (SELECT 1 FROM stored)
			UNION SELECT e.dst FROM walk w JOIN edges e ON e.src = w.ref)
		SELECT member FROM stored UNION ALL SELECT ref FROM walk ORDER BY 1`, key)
}

// keys returns the bytes of each entry of a list that has the form of a reference.
func keys(refs []string) [][]byte {
	out := make([][]byte, 0, len(refs))
	for _, ref := range refs {
		if key, ok := raw(ref); ok {
			out = append(out, key)
		}
	}
	return out
}

// Objects returns the canonical text of each descriptor of a list, in one statement (ops.BatchReader).
func (v view) Objects(refs []string) map[string][]byte {
	out := map[string][]byte{}
	if len(refs) == 0 {
		return out
	}
	v.each(func(rows pgx.Rows) error {
		var ref, text []byte
		if err := rows.Scan(&ref, &text); err != nil {
			return err
		}
		out[name(ref)] = text
		return nil
	}, `SELECT ref, canonical FROM objects WHERE ref = ANY($1::bytea[])`, keys(refs))
	return out
}

// Blobs returns the bytes of each digest of a list that the store has, in one statement (ops.BatchReader).
func (v view) Blobs(digests []string) map[string][]byte {
	out := make(map[string][]byte, len(digests))
	if len(digests) == 0 {
		return out
	}
	v.each(func(rows pgx.Rows) error {
		var digest []byte
		data := []byte{}
		if err := rows.Scan(&digest, &data); err != nil {
			return err
		}
		out[name(digest)] = data
		return nil
	}, `SELECT digest, data FROM payloads WHERE digest = ANY($1::bytea[])`, keys(digests))
	return out
}

// Deferred returns each descriptor of a list that a deferred record names (ops.BatchReader).
func (v view) Deferred(refs []string) map[string]bool {
	out := map[string]bool{}
	if len(refs) == 0 {
		return out
	}
	for _, ref := range v.refs(`SELECT DISTINCT ref FROM deferred WHERE ref = ANY($1::bytea[])`, keys(refs)) {
		out[ref] = true
	}
	return out
}

func (v view) ProducerObject(producer, key string) ([]byte, bool) {
	data := []byte{}
	if !v.row([]any{&data}, `SELECT data FROM producer_objects WHERE sha256(producer) = sha256($1) AND producer = $1 AND sha256(key) = sha256($2) AND key = $2`, []byte(producer), []byte(key)) {
		return nil, false
	}
	return data, true
}

func (v view) ProducerRemoved(producer, key string) bool {
	return v.exists(`SELECT 1 FROM producer_removals WHERE sha256(producer) = sha256($1) AND producer = $1 AND sha256(key) = sha256($2) AND key = $2`, []byte(producer), []byte(key))
}

func (v view) ProducerRemovals(producer string) []ops.Removal {
	var out []ops.Removal
	v.each(func(rows pgx.Rows) error {
		var r ops.Removal
		var key, reason []byte
		if err := rows.Scan(&r.Sequence, &key, &r.Time, &reason); err != nil {
			return err
		}
		r.Key, r.Reason = string(key), string(reason)
		out = append(out, r)
		return nil
	}, `SELECT sequence, key, time, reason FROM producer_removals WHERE sha256(producer) = sha256($1) AND producer = $1 ORDER BY sequence`, []byte(producer))
	return out
}

// ProducerEntryCount returns the last sequence number of a log. No statement removes an entry, and the
// sequence numbers have no gap (PRD-LOG-1), so this is the number of entries.
func (v view) ProducerEntryCount(producer, log string) int64 {
	var count int64
	v.row([]any{&count}, `SELECT coalesce(max(sequence), 0) FROM producer_entries WHERE sha256(producer) = sha256($1) AND producer = $1 AND sha256(log) = sha256($2) AND log = $2`,
		[]byte(producer), []byte(log))
	return count
}

// ProducerRemovalCount returns the last sequence number of the removal records of a producer (PRD-RMV-1).
func (v view) ProducerRemovalCount(producer string) int64 {
	var count int64
	v.row([]any{&count}, `SELECT coalesce(max(sequence), 0) FROM producer_removals WHERE sha256(producer) = sha256($1) AND producer = $1`, []byte(producer))
	return count
}

func (v view) ProducerEntries(producer, log string) []ops.Entry {
	var out []ops.Entry
	v.each(func(rows pgx.Rows) error {
		var e ops.Entry
		if err := rows.Scan(&e.Sequence, &e.Data); err != nil {
			return err
		}
		if e.Data == nil {
			e.Data = []byte{}
		}
		out = append(out, e)
		return nil
	}, `SELECT sequence, entry FROM producer_entries WHERE sha256(producer) = sha256($1) AND producer = $1 AND sha256(log) = sha256($2) AND log = $2 ORDER BY sequence`,
		[]byte(producer), []byte(log))
	return out
}

// session is the store for one call. Each read is one statement on the pool.
type session struct {
	view
	pool *pgxpool.Pool
	seen *seen // the removal epoch at the start of the call
}

// RemovalEpoch returns the removal epoch of the database (ops.RemovalEpoch). The session keeps it: an
// update of the session that finds a different epoch stops before its first write (ops.ErrRemoved).
func (s session) RemovalEpoch() int64 {
	var epoch int64
	s.row([]any{&epoch}, `SELECT epoch FROM removal_epoch`)
	s.seen.known, s.seen.epoch = true, epoch
	return epoch
}

// rollbackTime is the time that the rollback of a failed update can use.
const rollbackTime = 5 * time.Second

// Update runs fn in one transaction. The transaction commits when fn returns. If fn panics, or the commit
// fails, the transaction does not commit (STS-MAL-2).
func (s session) Update(fn func(tx ops.Tx)) {
	t, err := s.pool.Begin(s.ctx)
	if err != nil {
		ops.Raise(err)
	}
	committed := false
	defer func() {
		if !committed { // the context of the call can be at its deadline, so the rollback has its own
			ctx, cancel := context.WithTimeout(context.WithoutCancel(s.ctx), rollbackTime)
			defer cancel()
			_ = t.Rollback(ctx)
		}
	}()
	work := tx{view{s.ctx, t, s.life}, &tokenBatch{}, &gate{seen: *s.seen}}
	fn(work)
	work.flushTokens()
	if err := t.Commit(s.ctx); err != nil {
		ops.Raise(err)
	}
	committed = true
}

// tx is one update: a transaction at the READ COMMITTED level. After a lock, a read gives the committed
// state of each update that had the lock before.
type tx struct {
	view
	tokens *tokenBatch // the text index rows that the update did not write yet
	gate   *gate
}

// gate is the state of the removal lock of one update.
type gate struct {
	seen seen // the removal epoch at the start of the call of the session
	held bool // the update has the shared lock or the exclusive lock
}

// removalLock is the key of the advisory lock between the updates that write and the updates that remove.
const removalLock = 0x61617661_6e616d01

// enter takes the shared removal lock before the first write of an update, and then reads the removal
// epoch. An update of `remove` or of `gc_collect` has the exclusive lock (LockRemoval). Thus a write
// either commits before such an update starts, or it starts after such an update committed. In the second
// case, the checks of the call can have read content that the removal took, so the update stops with
// ops.ErrRemoved and the dispatch runs the call again. The two statements go in one round trip. The read
// of the epoch is a statement of its own, so its snapshot is later than the wait for the lock.
func (t tx) enter() {
	if t.gate.held {
		return
	}
	t.gate.held = true
	batch := &pgx.Batch{}
	batch.Queue(`SELECT pg_advisory_xact_lock_shared($1)`, int64(removalLock))
	batch.Queue(`SELECT epoch FROM removal_epoch`)
	results := t.q.(pgx.Tx).SendBatch(t.ctx, batch)
	_, err := results.Exec()
	var epoch int64
	if err == nil {
		err = results.QueryRow().Scan(&epoch)
	}
	if closeErr := results.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		ops.Raise(err)
	}
	if t.gate.seen.known && epoch != t.gate.seen.epoch {
		ops.Raise(ops.ErrRemoved)
	}
}

// LockRemoval takes the exclusive removal lock and permits a DELETE on the content tables in this
// transaction (ops.RemovalGate). It waits for each update that wrote, until it ends.
func (t tx) LockRemoval() {
	t.gate.held = true
	t.exec(`SELECT pg_advisory_xact_lock($1)`, int64(removalLock))
	t.exec(`SELECT set_config('aavanam.removal', 'on', true)`)
}

// AdvanceEpoch increments the removal epoch (ops.RemovalGate).
func (t tx) AdvanceEpoch() { t.exec(`UPDATE removal_epoch SET epoch = epoch + 1`) }

func (t tx) LockCollections(collections []string) {
	names := append([]string(nil), collections...)
	sort.Strings(names) // one order for each update, so two updates cannot wait for each other
	for _, c := range names {
		t.exec(`INSERT INTO collection_locks (collection) VALUES ($1) ON CONFLICT DO NOTHING`, []byte(c))
		t.exec(`SELECT 1 FROM collection_locks WHERE sha256(collection) = sha256($1) AND collection = $1 FOR UPDATE`, []byte(c))
	}
}

func (t tx) LockDigests(digests []string) {
	names := append([]string(nil), digests...)
	sort.Strings(names)
	for _, d := range names {
		key := must(d)
		var id int64
		for _, b := range key[:8] {
			id = id<<8 | int64(b)
		}
		t.exec(`SELECT pg_advisory_xact_lock($1)`, id)
	}
}

// LockProducer takes the lock of one producer name: an advisory lock on the first 8 bytes of the SHA-256
// digest of the name. Two names with one key only wait for each other.
func (t tx) LockProducer(producer string) {
	sum := sha256.Sum256([]byte(producer))
	t.exec(`SELECT pg_advisory_xact_lock($1, $2)`, int32(binary.BigEndian.Uint32(sum[:4])), int32(binary.BigEndian.Uint32(sum[4:8])))
}

func (t tx) PutObject(o ops.Object) {
	t.enter()
	var collection []byte
	if o.Kind == "snapshot" || o.Kind == "layer" { // the collection of a snapshot, or the base of a layer
		collection = []byte(o.Collection)
	}
	t.exec(`INSERT INTO objects (ref, kind, collection, canonical) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`,
		must(o.Ref), o.Kind, collection, o.Text)
	if len(o.Targets) > 0 {
		t.exec(`INSERT INTO edges (src, dst) SELECT $1, unnest($2::bytea[]) ON CONFLICT DO NOTHING`, must(o.Ref), raws(o.Targets))
	}
}

func (t tx) PutBlob(digest string, data []byte) {
	t.enter()
	if data == nil {
		data = []byte{}
	}
	t.exec(`INSERT INTO payloads (digest, size, data) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, must(digest), len(data), data)
}

func (t tx) ClearDeferred(digest string) {
	t.exec(`DELETE FROM deferred WHERE digest = $1`, must(digest))
}

func (t tx) PutDeferred(digest, ref string) {
	t.enter()
	t.exec(`INSERT INTO deferred (digest, ref) VALUES ($1, $2) ON CONFLICT DO NOTHING`, must(digest), must(ref))
}

func (t tx) DeclareGate(collection string) {
	t.exec(`INSERT INTO gates (collection) VALUES ($1) ON CONFLICT DO NOTHING`, []byte(collection))
}

func (t tx) DeclareSection(collection, section string) {
	t.exec(`INSERT INTO sections (collection, section) VALUES ($1, $2) ON CONFLICT DO NOTHING`, []byte(collection), section)
}

func (t tx) PutCandidate(c ops.Candidate) {
	t.enter()
	t.exec(`INSERT INTO candidates (id, snapshot, collection, policy, expected_head) VALUES ($1, $2, $3, $4, $5) ON CONFLICT DO NOTHING`,
		must(c.ID), must(c.Snapshot), []byte(c.Collection), []byte(c.Policy), optional(c.ExpectedHead))
}

func (t tx) Publish(p ops.Publication) {
	t.enter()
	t.exec(`INSERT INTO heads (collection, snapshot) VALUES ($1, $2) ON CONFLICT ((sha256(collection))) DO UPDATE SET snapshot = EXCLUDED.snapshot`,
		[]byte(p.Collection), must(p.Head))
	t.exec(`INSERT INTO publications (collection, candidate, receipt, activation, previous_head, head) VALUES ($1, $2, $3, $4, $5, $6)`,
		[]byte(p.Collection), must(p.Candidate), must(p.Receipt), optional(p.Activation), optional(p.PreviousHead), must(p.Head))
}

// PutContext adds a read context. It writes the closure of each root that has no stored closure, in one
// recursive statement (KND-DEP-3). A stored closure is complete: one statement writes it, and the references
// of the closure of a root do not change, also after a removal (REL-RMV-8). Two updates that write one
// closure write the same rows. The statement writes the rows in ascending order, so two updates with the
// same roots in a different sequence do not wait for each other in a circle.
//
// The column `gone` gets the removed objects of the closures: each reference of a removal record that
// names no object now. Only a removal leaves a reference with no object, so the read uses the removal
// records and not each member.
func (t tx) PutContext(id string, record ops.ContextRecord) {
	t.enter()
	capabilities := record.Capabilities
	if capabilities == nil {
		capabilities = []string{}
	}
	roots := raws(record.Roots)
	if len(roots) > 0 {
		t.exec(`INSERT INTO closures (root, member)
			WITH RECURSIVE walk(root, ref) AS (
				SELECT r.root, r.root FROM unnest($1::bytea[]) AS r(root)
					WHERE NOT EXISTS (SELECT 1 FROM closures c WHERE c.root = r.root)
				UNION
				SELECT w.root, e.dst FROM walk w JOIN edges e ON e.src = w.ref)
			SELECT root, ref FROM walk ORDER BY root, ref ON CONFLICT DO NOTHING`, roots)
	}
	var expires *int64
	if record.Expires != 0 {
		expires = &record.Expires
	}
	t.exec(`INSERT INTO contexts (id, roots, capabilities, expires, gone)
		SELECT $1, $2::bytea[], $3::text[], $4::bigint, (SELECT array_agg(DISTINCT r.ref) FROM removals r
			WHERE NOT EXISTS (SELECT 1 FROM objects o WHERE o.ref = r.ref)
			AND EXISTS (SELECT 1 FROM closures c WHERE c.root = ANY($2::bytea[]) AND c.member = r.ref))`,
		[]byte(id), roots, capabilities, expires)
}

func (t tx) DeleteContexts(ids []string) {
	if len(ids) == 0 {
		return
	}
	keys := make([][]byte, len(ids))
	for i, id := range ids {
		keys[i] = []byte(id)
	}
	t.exec(`DELETE FROM contexts WHERE id = ANY($1::bytea[])`, keys)
}

// The removal methods need LockRemoval first: the trigger of a content table refuses a DELETE of each
// other transaction.

func (t tx) DeleteObjects(refs []string) {
	if len(refs) > 0 {
		t.exec(`DELETE FROM objects WHERE ref = ANY($1::bytea[])`, raws(refs))
	}
}

func (t tx) DeleteEdges(sources []string) {
	if len(sources) == 0 {
		return
	}
	keys := raws(sources)
	t.exec(`DELETE FROM edges WHERE src = ANY($1::bytea[])`, keys)
	t.exec(`DELETE FROM closures WHERE root = ANY($1::bytea[]) OR member = ANY($1::bytea[])`, keys)
}

func (t tx) DeleteDeferred(refs []string) {
	if len(refs) > 0 {
		t.exec(`DELETE FROM deferred WHERE ref = ANY($1::bytea[])`, raws(refs))
	}
}

// DeleteBlobs removes the bytes, the text index row and the row cache of each digest.
func (t tx) DeleteBlobs(digests []string) {
	if len(digests) == 0 {
		return
	}
	t.flushTokens()
	keys := raws(digests)
	for _, table := range []string{"payloads", "text_tokens", "table_heads", "table_rows"} {
		t.exec(`DELETE FROM `+table+` WHERE digest = ANY($1::bytea[])`, keys)
	}
}

func (t tx) DeleteCandidates(ids []string) {
	if len(ids) > 0 {
		t.exec(`DELETE FROM candidates WHERE id = ANY($1::bytea[])`, raws(ids))
	}
}

func (t tx) PutRemoval(r ops.RemovalRecord) {
	t.exec(`INSERT INTO removals (sequence, ref, kind, time, reason) VALUES ($1, $2, $3, $4, $5)`,
		r.Sequence, must(r.Ref), r.Kind, r.Time, []byte(r.Reason))
}

func (t tx) PutProducerObject(producer, key string, data []byte) {
	if data == nil {
		data = []byte{}
	}
	t.exec(`INSERT INTO producer_objects (producer, key, data) VALUES ($1, $2, $3)`, []byte(producer), []byte(key), data)
}

func (t tx) DeleteProducerObject(producer, key string) {
	t.exec(`DELETE FROM producer_objects WHERE sha256(producer) = sha256($1) AND producer = $1 AND sha256(key) = sha256($2) AND key = $2`, []byte(producer), []byte(key))
}

func (t tx) PutProducerRemoval(producer string, r ops.Removal) {
	t.exec(`INSERT INTO producer_removals (producer, sequence, key, time, reason) VALUES ($1, $2, $3, $4, $5)`,
		[]byte(producer), r.Sequence, []byte(r.Key), r.Time, []byte(r.Reason))
}

func (t tx) PutProducerEntry(producer, log string, e ops.Entry) {
	data := e.Data
	if data == nil {
		data = []byte{}
	}
	t.exec(`INSERT INTO producer_entries (producer, log, sequence, entry) VALUES ($1, $2, $3, $4)`,
		[]byte(producer), []byte(log), e.Sequence, data)
}
