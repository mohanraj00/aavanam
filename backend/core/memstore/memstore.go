// Package memstore is a storage in memory, for tests and for the conformance driver. It keeps nothing
// after the process ends.
package memstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/mohanraj00/aavanam/backend/core/ops"
)

type object struct {
	kind       string
	text       []byte
	collection string // the collection of a snapshot, or the base collection of a layer
}

type producerKey struct{ producer, key string }

type logKey struct{ producer, log string }

type state struct {
	advance    bool // the update removed content: its commit increments the removal epoch
	objects    map[string]object
	targets    map[string][]string
	sources    map[string]map[string]struct{}
	snapshots  map[string]int // the number of snapshots of each collection
	layers     map[string]int // the number of layers of each base collection (LYR-DEF-5)
	blobs      map[string][]byte
	deferred   map[string]map[string]struct{} // digest, then ref
	heads      map[string]string
	sections   map[string]string
	gates      map[string]struct{}
	candidates map[string]ops.Candidate
	published  map[string]struct{}
	log        []ops.Publication
	contexts   map[string]ops.ContextRecord
	members    map[string]map[string]struct{}
	producer   map[producerKey][]byte
	removals   map[string][]ops.Removal
	entries    map[logKey][]ops.Entry
	gone       []ops.RemovalRecord // the removal records of the Evidence section (REL-RMV-5)
	imports    [][]string          // the roots of each `ok` import of a device store (BUN-RMV-1)
	tokens     map[string][]uint64 // derived: the different word tokens of a text digest, in ascending order
	tables     map[string]*table   // derived: the columns and the rows of a table digest
}

// table is one entry of the row cache (ops.RowCache).
type table struct {
	columns []string
	rows    [][]byte
}

func newState() *state {
	return &state{
		objects: map[string]object{}, targets: map[string][]string{}, sources: map[string]map[string]struct{}{},
		snapshots: map[string]int{}, layers: map[string]int{}, blobs: map[string][]byte{}, deferred: map[string]map[string]struct{}{},
		heads: map[string]string{}, sections: map[string]string{}, gates: map[string]struct{}{},
		candidates: map[string]ops.Candidate{}, published: map[string]struct{}{}, contexts: map[string]ops.ContextRecord{},
		members: map[string]map[string]struct{}{}, producer: map[producerKey][]byte{}, removals: map[string][]ops.Removal{},
		entries: map[logKey][]ops.Entry{}, tokens: map[string][]uint64{}, tables: map[string]*table{},
	}
}

// Store is one store in memory. A local output store reads the content of its device store (BUN-IMP-8).
type Store struct {
	mu     sync.RWMutex
	st     *state
	device *Store
	output *Store // the local output store of a device store, or nil
	// epoch is the number of updates of this store that removed content (ops.RemovalEpoch).
	epoch atomic.Int64
}

// removals returns the removal epoch that a call of the store reads: the epoch of the store plus the epoch
// of its device store, because a local output store reads the objects of its device store (BUN-IMP-8).
func (s *Store) removals() int64 {
	if s.device != nil {
		return s.epoch.Load() + s.device.epoch.Load()
	}
	return s.epoch.Load()
}

// seen has the removal epoch at the start of a call.
type seen struct {
	known bool
	epoch int64
}

// New returns an empty store.
func New() *Store { return &Store{st: newState()} }

// Output returns an empty local output store of a device store. A device store has one local output
// store (BUN-IMP-5), so the application calls it one time.
func (s *Store) Output() *Store {
	out := &Store{st: newState(), device: s}
	s.mu.Lock()
	s.output = out
	s.mu.Unlock()
	return out
}

// Reset removes each record of the store.
func (s *Store) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st = newState()
}

// Session returns the store for one call. A storage in memory has no deadline.
func (s *Store) Session(context.Context) ops.Session { return session{s, &seen{}} }

// session reads with the read lock of the store. An update has the write lock for all of its work.
type session struct {
	s    *Store
	seen *seen // the removal epoch at the start of the call
}

// RemovalEpoch returns the removal epoch of the store (ops.RemovalEpoch). The session keeps it: an update
// of the session that finds a different epoch stops before its first write (ops.ErrRemoved).
func (v session) RemovalEpoch() int64 {
	epoch := v.s.removals()
	v.seen.known, v.seen.epoch = true, epoch
	return epoch
}

func (v session) Update(fn func(tx ops.Tx)) {
	if v.s.device != nil { // the order is always the device store, then the output store
		v.s.device.mu.RLock()
		defer v.s.device.mu.RUnlock()
	}
	v.s.mu.Lock()
	defer v.s.mu.Unlock()
	// The checks of the call read the store before this update had the lock. If an update removed content
	// after the start of the call, the dispatch runs the call again (REL-RMV-4, LIF-GC-5).
	if v.seen.known && v.s.removals() != v.seen.epoch {
		ops.Raise(ops.ErrRemoved)
	}
	work := tx{view{v.s, v.s.st.clone()}}
	fn(work)
	if work.st.advance {
		work.st.advance = false
		v.s.epoch.Add(1)
	}
	v.s.st = work.st // the commit. A panic of fn keeps the old state.
}

func copyMap[K comparable, V any](m map[K]V) map[K]V {
	out := make(map[K]V, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	return out
}

// clone returns a copy of the state for one update. The copy shares each inner map, each list and each
// byte sequence with the state, so an update makes a new one before a change.
func (st *state) clone() *state {
	return &state{
		objects: copyMap(st.objects), targets: copyMap(st.targets), sources: copyMap(st.sources),
		snapshots: copyMap(st.snapshots), layers: copyMap(st.layers), blobs: copyMap(st.blobs), deferred: copyMap(st.deferred),
		heads: copyMap(st.heads), sections: copyMap(st.sections), gates: copyMap(st.gates),
		candidates: copyMap(st.candidates), published: copyMap(st.published), log: st.log[:len(st.log):len(st.log)],
		contexts: copyMap(st.contexts), members: copyMap(st.members), producer: copyMap(st.producer),
		removals: copyMap(st.removals), entries: copyMap(st.entries), tokens: copyMap(st.tokens), tables: copyMap(st.tables),
		gone: st.gone[:len(st.gone):len(st.gone)], imports: st.imports[:len(st.imports):len(st.imports)],
	}
}

// tx is one update. It changes a copy of the state.
type tx struct{ view }

func (t tx) LockCollections([]string) {} // an update has the write lock of the store

// LockRemoval has no effect: an update has the write lock of the store (ops.RemovalGate).
func (t tx) LockRemoval() {}

// AdvanceEpoch increments the removal epoch when the update commits (ops.RemovalGate).
func (t tx) AdvanceEpoch() { t.st.advance = true }

func (t tx) LockDigests([]string) {}

func (t tx) PutObject(o ops.Object) {
	if _, _, ok := t.Object(o.Ref); ok {
		return // the store has the descriptor, or its device store has it (IDN-CAN-3, BUN-IMP-8)
	}
	// A removal keeps the `refs` of an object, so a second write of a removed descriptor finds them (REL-RMV-11).
	t.st.objects[o.Ref] = object{o.Kind, append([]byte(nil), o.Text...), o.Collection}
	t.st.targets[o.Ref] = append([]string(nil), o.Targets...)
	sort.Strings(t.st.targets[o.Ref])
	for _, target := range o.Targets {
		set := copyMap(t.st.sources[target])
		set[o.Ref] = struct{}{}
		t.st.sources[target] = set
	}
	switch o.Kind {
	case "snapshot":
		t.st.snapshots[o.Collection]++
	case "layer":
		t.st.layers[o.Collection]++
	}
}

func (t tx) PutBlob(digest string, data []byte) {
	if _, ok := t.Blob(digest); !ok {
		t.st.blobs[digest] = append([]byte(nil), data...)
	}
}

func (t tx) ClearDeferred(digest string) { delete(t.st.deferred, digest) }

func (t tx) PutDeferred(digest, ref string) {
	set := copyMap(t.st.deferred[digest])
	set[ref] = struct{}{}
	t.st.deferred[digest] = set
}

func (t tx) DeclareGate(collection string) { t.st.gates[collection] = struct{}{} }

func (t tx) DeclareSection(collection, section string) { t.st.sections[collection] = section }

func (t tx) PutCandidate(c ops.Candidate) {
	if _, ok := t.st.candidates[c.ID]; !ok {
		t.st.candidates[c.ID] = c
	}
}

func (t tx) Publish(p ops.Publication) {
	t.st.heads[p.Collection] = p.Head
	t.st.published[p.Candidate] = struct{}{}
	t.st.log = append(t.st.log, p)
}

// PutContext adds a read context with the closure of its roots as its content, without the removed objects
// (CTX-OPN-2, REL-RMV-8).
func (t tx) PutContext(id string, record ops.ContextRecord) {
	members := ops.ClosureIn(t, record.Roots)
	for _, ref := range t.RemovedRefs() {
		delete(members, ref)
	}
	t.st.contexts[id] = record
	t.st.members[id] = members
}

func (t tx) DeleteContexts(ids []string) {
	for _, id := range ids {
		delete(t.st.contexts, id)
		delete(t.st.members, id)
	}
}

// DeleteObjects removes the descriptors. The entries of `targets` and `sources` stay (REL-RMV-8).
func (t tx) DeleteObjects(refs []string) {
	for _, ref := range refs {
		o, ok := t.st.objects[ref]
		if !ok {
			continue
		}
		switch o.kind {
		case "snapshot":
			t.st.snapshots[o.collection]--
		case "layer":
			t.st.layers[o.collection]--
		}
		delete(t.st.objects, ref)
	}
}

func (t tx) DeleteEdges(sources []string) {
	for _, ref := range sources {
		for _, target := range t.st.targets[ref] {
			set := copyMap(t.st.sources[target])
			delete(set, ref)
			if len(set) == 0 {
				delete(t.st.sources, target)
			} else {
				t.st.sources[target] = set
			}
		}
		delete(t.st.targets, ref)
	}
}

func (t tx) DeleteDeferred(refs []string) {
	for digest, waiting := range t.st.deferred {
		var set map[string]struct{}
		for _, ref := range refs {
			if _, ok := waiting[ref]; ok {
				if set == nil {
					set = copyMap(waiting)
				}
				delete(set, ref)
			}
		}
		switch {
		case set == nil:
		case len(set) == 0:
			delete(t.st.deferred, digest)
		default:
			t.st.deferred[digest] = set
		}
	}
}

// DeleteBlobs removes the bytes, the tokens and the table of each digest.
func (t tx) DeleteBlobs(digests []string) {
	for _, digest := range digests {
		delete(t.st.blobs, digest)
		delete(t.st.tokens, digest)
		delete(t.st.tables, digest)
	}
}

func (t tx) DeleteCandidates(ids []string) {
	for _, id := range ids {
		delete(t.st.candidates, id)
	}
}

func (t tx) PutRemoval(r ops.RemovalRecord) { t.st.gone = append(t.st.gone, r) }

// PutImport adds the record of one import (ops.ImportLog).
func (t tx) PutImport(roots []string) {
	t.st.imports = append(t.st.imports, append([]string(nil), roots...))
}

// Output returns a reader of the local output store (ops.OutputView). The update has the write lock of the
// device store, and each read and each update of the local output store takes the read lock of the device
// store first. Thus the local output store does not change until the update ends.
func (t tx) Output() (ops.Reader, bool) {
	if t.s.output == nil {
		return nil, false
	}
	return view{t.s.output, t.s.output.st}, true
}

// tokenValue returns the number of a token of package text: the letter `t` and 16 hexadecimal digits.
func tokenValue(token string) uint64 {
	if len(token) != 17 {
		ops.Raise(fmt.Errorf("the token %q does not have 17 characters", token))
	}
	value, err := strconv.ParseUint(token[1:], 16, 64)
	if err != nil {
		ops.Raise(err)
	}
	return value
}

// PutTextTokens records the different tokens of a digest (ops.TextIndex).
func (t tx) PutTextTokens(digest string, tokens []string) {
	if t.TextIndexed(digest) {
		return
	}
	set := make([]uint64, 0, len(tokens))
	for _, token := range tokens {
		set = append(set, tokenValue(token))
	}
	slices.Sort(set)
	t.st.tokens[digest] = slices.Clip(slices.Compact(set))
}

// PutTable records the columns and the rows of a table (ops.RowCache).
func (t tx) PutTable(digest string, columns []string, rows [][]byte) {
	if _, _, ok := t.TableHead(digest); ok {
		return
	}
	entry := &table{columns: append([]string(nil), columns...), rows: make([][]byte, len(rows))}
	for i, row := range rows {
		entry.rows[i] = append([]byte(nil), row...)
	}
	t.st.tables[digest] = entry
}

func (t tx) PutProducerObject(producer, key string, data []byte) {
	t.st.producer[producerKey{producer, key}] = append([]byte(nil), data...)
}

func (t tx) DeleteProducerObject(producer, key string) {
	delete(t.st.producer, producerKey{producer, key})
}

func (t tx) PutProducerRemoval(producer string, r ops.Removal) {
	old := t.st.removals[producer]
	t.st.removals[producer] = append(old[:len(old):len(old)], r)
}

func (t tx) PutProducerEntry(producer, log string, e ops.Entry) {
	k := logKey{producer, log}
	old := t.st.entries[k]
	e.Data = append([]byte(nil), e.Data...)
	t.st.entries[k] = append(old[:len(old):len(old)], e)
}

func (v session) read() (view, func()) {
	if v.s.device != nil {
		v.s.device.mu.RLock()
	}
	v.s.mu.RLock()
	return view{v.s, v.s.st}, func() {
		v.s.mu.RUnlock()
		if v.s.device != nil {
			v.s.device.mu.RUnlock()
		}
	}
}

// view reads one state with no lock. The caller has the lock.
type view struct {
	s  *Store
	st *state
}

func (v view) below() (view, bool) {
	if v.s.device == nil {
		return view{}, false
	}
	return view{v.s.device, v.s.device.st}, true
}

func keys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func union(a, b []string) []string {
	set := map[string]struct{}{}
	for _, list := range [][]string{a, b} {
		for _, x := range list {
			set[x] = struct{}{}
		}
	}
	return keys(set)
}

func (v view) Object(ref string) (string, []byte, bool) {
	if o, ok := v.st.objects[ref]; ok {
		return o.kind, o.text, true
	}
	if d, ok := v.below(); ok {
		return d.Object(ref)
	}
	return "", nil, false
}

func (v view) Targets(ref string) []string {
	out := v.st.targets[ref]
	if d, ok := v.below(); ok {
		return union(out, d.Targets(ref))
	}
	return append([]string(nil), out...)
}

func (v view) Sources(ref string) []string {
	out := keys(v.st.sources[ref])
	if d, ok := v.below(); ok {
		return union(out, d.Sources(ref))
	}
	return out
}

func (v view) Blob(digest string) ([]byte, bool) {
	if data, ok := v.st.blobs[digest]; ok {
		return data, true
	}
	if d, ok := v.below(); ok {
		return d.Blob(digest)
	}
	return nil, false
}

func (v view) HasDeferred(ref string) bool {
	for _, refs := range v.st.deferred {
		if _, ok := refs[ref]; ok {
			return true
		}
	}
	d, ok := v.below()
	return ok && d.HasDeferred(ref)
}

func (v view) DeferredRefs(digest string) []string {
	out := keys(v.st.deferred[digest])
	if d, ok := v.below(); ok {
		return union(out, d.DeferredRefs(digest))
	}
	return out
}

func (v view) HasSnapshot(collection string) bool { return v.st.snapshots[collection] > 0 }

func (v view) HasLayer(collection string) bool { return v.st.layers[collection] > 0 }

func (v view) Heads(collections []string) map[string]string {
	out := map[string]string{}
	for _, c := range collections {
		if head, ok := v.st.heads[c]; ok {
			out[c] = head
		}
	}
	return out
}

func (v view) Section(collection string) (string, bool) {
	section, ok := v.st.sections[collection]
	return section, ok
}

func (v view) Gated(collection string) bool {
	_, ok := v.st.gates[collection]
	return ok
}

func (v view) Candidate(id string) (ops.Candidate, bool) {
	c, ok := v.st.candidates[id]
	return c, ok
}

func (v view) Published(candidate string) bool {
	_, ok := v.st.published[candidate]
	return ok
}

func (v view) Publications(collection string) []ops.Publication {
	var out []ops.Publication
	for _, p := range v.st.log {
		if p.Collection == collection {
			out = append(out, p)
		}
	}
	return out
}

func (v view) Context(id string) (ops.ContextRecord, bool) {
	record, ok := v.st.contexts[id]
	return record, ok
}

func (v view) IsMember(context, ref string) bool {
	_, ok := v.st.members[context][ref]
	return ok
}

func (v view) Members(context string) []string { return keys(v.st.members[context]) }

func (v view) ContextsWith(ref string) []string {
	var out []string
	for id, members := range v.st.members {
		if _, ok := members[ref]; ok {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

func (v view) OpenRoots(now int64) []string {
	set := map[string]struct{}{}
	for _, record := range v.st.contexts {
		if record.Expires == 0 || record.Expires > now {
			for _, root := range record.Roots {
				set[root] = struct{}{}
			}
		}
	}
	return keys(set)
}

func (v view) EndedContexts(now int64) []string {
	var out []string
	for id, record := range v.st.contexts {
		if record.Expires != 0 && record.Expires <= now {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

func (v view) Removals() []ops.RemovalRecord { return append([]ops.RemovalRecord(nil), v.st.gone...) }

func (v view) HasRemoval(ref string) bool {
	for _, r := range v.st.gone {
		if r.Ref == ref {
			return true
		}
	}
	return false
}

func (v view) RemovedRefs() []string {
	set := map[string]struct{}{}
	for _, r := range v.st.gone {
		if _, ok := v.st.objects[r.Ref]; !ok {
			set[r.Ref] = struct{}{}
		}
	}
	return keys(set)
}

// OwnObjects gives the objects of this store only, not those of its device store.
func (v view) OwnObjects() []ops.Listed {
	out := make([]ops.Listed, 0, len(v.st.objects))
	for ref, o := range v.st.objects {
		out = append(out, ops.Listed{Ref: ref, Kind: o.kind})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ref < out[j].Ref })
	return out
}

func (v view) OwnBlobs() []string {
	out := make([]string, 0, len(v.st.blobs))
	for digest := range v.st.blobs {
		out = append(out, digest)
	}
	sort.Strings(out)
	return out
}

func (v view) Naming(digest string) []string {
	var out []string
	for ref, o := range v.st.objects {
		if bytes.Contains(o.text, []byte(digest)) {
			out = append(out, ref)
		}
	}
	sort.Strings(out)
	return out
}

func (v view) AllHeads() map[string]string { return copyMap(v.st.heads) }

func (v view) Candidates() []ops.Candidate {
	out := make([]ops.Candidate, 0, len(v.st.candidates))
	for _, c := range v.st.candidates {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// ImportRoots returns each root of each recorded import (ops.ImportLog).
func (v view) ImportRoots() []string {
	set := map[string]struct{}{}
	for _, roots := range v.st.imports {
		for _, root := range roots {
			set[root] = struct{}{}
		}
	}
	return keys(set)
}

// TextIndexed tells if the store, or its device store, has the tokens of a digest.
func (v view) TextIndexed(digest string) bool {
	if _, ok := v.st.tokens[digest]; ok {
		return true
	}
	d, ok := v.below()
	return ok && d.TextIndexed(digest)
}

func (v view) tokensOf(digest string) ([]uint64, bool) {
	if set, ok := v.st.tokens[digest]; ok {
		return set, true
	}
	if d, ok := v.below(); ok {
		return d.tokensOf(digest)
	}
	return nil, false
}

// TextCandidates gives each indexed digest of the list that has each token (ops.TextIndex). The index
// keeps no word order, so a digest with the words in a different order is a candidate too.
func (v view) TextCandidates(tokens, digests []string) (candidates, unindexed []string) {
	want := make([]uint64, len(tokens))
	for i, token := range tokens {
		want[i] = tokenValue(token)
	}
	for _, digest := range digests {
		set, ok := v.tokensOf(digest)
		if !ok {
			unindexed = append(unindexed, digest)
			continue
		}
		all := true
		for _, token := range want {
			if _, found := slices.BinarySearch(set, token); !found {
				all = false
				break
			}
		}
		if all {
			candidates = append(candidates, digest)
		}
	}
	return candidates, unindexed
}

func (v view) tableOf(digest string) (*table, bool) {
	if entry, ok := v.st.tables[digest]; ok {
		return entry, true
	}
	if d, ok := v.below(); ok {
		return d.tableOf(digest)
	}
	return nil, false
}

func (v view) TableHead(digest string) ([]string, int, bool) {
	entry, ok := v.tableOf(digest)
	if !ok {
		return nil, 0, false
	}
	return append([]string(nil), entry.columns...), len(entry.rows), true
}

func (v view) TableRow(digest string, index int) ([]byte, bool) {
	entry, ok := v.tableOf(digest)
	if !ok || index < 0 || index >= len(entry.rows) {
		return nil, false
	}
	return entry.rows[index], true
}

func (v view) TableRows(digest string) [][]byte {
	entry, _ := v.tableOf(digest)
	if entry == nil {
		return nil
	}
	return entry.rows[:len(entry.rows):len(entry.rows)]
}

func (v view) ProducerObject(producer, key string) ([]byte, bool) {
	data, ok := v.st.producer[producerKey{producer, key}]
	return data, ok
}

func (v view) ProducerRemoved(producer, key string) bool {
	for _, r := range v.st.removals[producer] {
		if r.Key == key {
			return true
		}
	}
	return false
}

func (v view) ProducerRemovals(producer string) []ops.Removal {
	return append([]ops.Removal(nil), v.st.removals[producer]...)
}

func (v view) ProducerEntries(producer, log string) []ops.Entry {
	return append([]ops.Entry(nil), v.st.entries[logKey{producer, log}]...)
}

// The reads of a session take the lock for one read.

func (v session) Object(ref string) (string, []byte, bool) {
	r, done := v.read()
	defer done()
	return r.Object(ref)
}

func (v session) Targets(ref string) []string {
	r, done := v.read()
	defer done()
	return r.Targets(ref)
}

func (v session) Sources(ref string) []string {
	r, done := v.read()
	defer done()
	return r.Sources(ref)
}

func (v session) Blob(digest string) ([]byte, bool) {
	r, done := v.read()
	defer done()
	return r.Blob(digest)
}

func (v session) HasDeferred(ref string) bool {
	r, done := v.read()
	defer done()
	return r.HasDeferred(ref)
}

func (v session) DeferredRefs(digest string) []string {
	r, done := v.read()
	defer done()
	return r.DeferredRefs(digest)
}

func (v session) HasSnapshot(collection string) bool {
	r, done := v.read()
	defer done()
	return r.HasSnapshot(collection)
}

func (v session) HasLayer(collection string) bool {
	r, done := v.read()
	defer done()
	return r.HasLayer(collection)
}

func (v session) Heads(collections []string) map[string]string {
	r, done := v.read()
	defer done()
	return r.Heads(collections)
}

func (v session) Section(collection string) (string, bool) {
	r, done := v.read()
	defer done()
	return r.Section(collection)
}

func (v session) Gated(collection string) bool {
	r, done := v.read()
	defer done()
	return r.Gated(collection)
}

func (v session) Candidate(id string) (ops.Candidate, bool) {
	r, done := v.read()
	defer done()
	return r.Candidate(id)
}

func (v session) Published(candidate string) bool {
	r, done := v.read()
	defer done()
	return r.Published(candidate)
}

func (v session) Publications(collection string) []ops.Publication {
	r, done := v.read()
	defer done()
	return r.Publications(collection)
}

func (v session) Context(id string) (ops.ContextRecord, bool) {
	r, done := v.read()
	defer done()
	return r.Context(id)
}

func (v session) IsMember(context, ref string) bool {
	r, done := v.read()
	defer done()
	return r.IsMember(context, ref)
}

func (v session) Members(context string) []string {
	r, done := v.read()
	defer done()
	return r.Members(context)
}

func (v session) ContextsWith(ref string) []string {
	r, done := v.read()
	defer done()
	return r.ContextsWith(ref)
}

func (v session) OpenRoots(now int64) []string {
	r, done := v.read()
	defer done()
	return r.OpenRoots(now)
}

func (v session) EndedContexts(now int64) []string {
	r, done := v.read()
	defer done()
	return r.EndedContexts(now)
}

func (v session) Removals() []ops.RemovalRecord {
	r, done := v.read()
	defer done()
	return r.Removals()
}

func (v session) HasRemoval(ref string) bool {
	r, done := v.read()
	defer done()
	return r.HasRemoval(ref)
}

func (v session) RemovedRefs() []string {
	r, done := v.read()
	defer done()
	return r.RemovedRefs()
}

func (v session) OwnObjects() []ops.Listed {
	r, done := v.read()
	defer done()
	return r.OwnObjects()
}

func (v session) OwnBlobs() []string {
	r, done := v.read()
	defer done()
	return r.OwnBlobs()
}

func (v session) Naming(digest string) []string {
	r, done := v.read()
	defer done()
	return r.Naming(digest)
}

func (v session) AllHeads() map[string]string {
	r, done := v.read()
	defer done()
	return r.AllHeads()
}

func (v session) Candidates() []ops.Candidate {
	r, done := v.read()
	defer done()
	return r.Candidates()
}

func (v session) TextIndexed(digest string) bool {
	r, done := v.read()
	defer done()
	return r.TextIndexed(digest)
}

func (v session) TextCandidates(tokens, digests []string) (candidates, unindexed []string) {
	r, done := v.read()
	defer done()
	return r.TextCandidates(tokens, digests)
}

// PutTextTokens has an effect only in an update.
func (v session) PutTextTokens(string, []string) {}

func (v session) TableHead(digest string) ([]string, int, bool) {
	r, done := v.read()
	defer done()
	return r.TableHead(digest)
}

func (v session) TableRow(digest string, index int) ([]byte, bool) {
	r, done := v.read()
	defer done()
	return r.TableRow(digest, index)
}

func (v session) TableRows(digest string) [][]byte {
	r, done := v.read()
	defer done()
	return r.TableRows(digest)
}

// PutTable has an effect only in an update.
func (v session) PutTable(string, []string, [][]byte) {}

func (v session) ProducerObject(producer, key string) ([]byte, bool) {
	r, done := v.read()
	defer done()
	return r.ProducerObject(producer, key)
}

func (v session) ProducerRemoved(producer, key string) bool {
	r, done := v.read()
	defer done()
	return r.ProducerRemoved(producer, key)
}

func (v session) ProducerRemovals(producer string) []ops.Removal {
	r, done := v.read()
	defer done()
	return r.ProducerRemovals(producer)
}

func (v session) ProducerEntries(producer, log string) []ops.Entry {
	r, done := v.read()
	defer done()
	return r.ProducerEntries(producer, log)
}

// Fingerprint returns a digest of the content, the catalog, the producer records and the removal records of
// the store. It does not include the read contexts, the gates, the sections or the import records. Two
// equal stores have equal fingerprints.
func (s *Store) Fingerprint() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st := s.st
	var lines []string
	add := func(table string, parts ...any) { lines = append(lines, fmt.Sprintf("%s:%q", table, parts)) }
	for ref, o := range st.objects {
		add("objects", ref, o.kind, o.text)
	}
	for digest, data := range st.blobs {
		add("blobs", digest, sha256.Sum256(data), len(data))
	}
	for digest, refs := range st.deferred {
		for ref := range refs {
			add("deferred", digest, ref)
		}
	}
	for _, c := range st.candidates {
		add("candidates", c)
	}
	for collection, head := range st.heads {
		add("heads", collection, head)
	}
	for n, p := range st.log {
		add("publications", n, p)
	}
	for k, data := range st.producer {
		add("producer_objects", k.producer, k.key, data)
	}
	for k, entries := range st.entries {
		for _, e := range entries {
			add("producer_entries", k.producer, k.log, e.Sequence, e.Data)
		}
	}
	for producer, removals := range st.removals {
		for _, r := range removals {
			add("producer_removals", producer, r)
		}
	}
	for _, r := range st.gone {
		add("removals", r)
	}
	sort.Strings(lines)
	h := sha256.New()
	for _, line := range lines {
		h.Write([]byte(line))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}
