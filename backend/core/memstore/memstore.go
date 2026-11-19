// Package memstore is a storage in memory, for tests and for the conformance driver. It keeps nothing
// after the process ends.
package memstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"

	"github.com/mohanraj00/aavanam/backend/core/ops"
)

type object struct {
	kind string
	text []byte
}

type producerKey struct{ producer, key string }

type logKey struct{ producer, log string }

type state struct {
	objects    map[string]object
	targets    map[string][]string
	sources    map[string]map[string]struct{}
	snapshots  map[string]int // the number of snapshots of each collection
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
}

func newState() *state {
	return &state{
		objects: map[string]object{}, targets: map[string][]string{}, sources: map[string]map[string]struct{}{},
		snapshots: map[string]int{}, blobs: map[string][]byte{}, deferred: map[string]map[string]struct{}{},
		heads: map[string]string{}, sections: map[string]string{}, gates: map[string]struct{}{},
		candidates: map[string]ops.Candidate{}, published: map[string]struct{}{}, contexts: map[string]ops.ContextRecord{},
		members: map[string]map[string]struct{}{}, producer: map[producerKey][]byte{}, removals: map[string][]ops.Removal{},
		entries: map[logKey][]ops.Entry{},
	}
}

// Store is one store in memory. A local output store reads the content of its device store (BUN-IMP-8).
type Store struct {
	mu     sync.RWMutex
	st     *state
	device *Store
}

// New returns an empty store.
func New() *Store { return &Store{st: newState()} }

// Output returns an empty local output store of a device store.
func (s *Store) Output() *Store { return &Store{st: newState(), device: s} }

// Reset removes each record of the store.
func (s *Store) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st = newState()
}

// Session returns the store for one call. A storage in memory has no deadline.
func (s *Store) Session(context.Context) ops.Session { return session{s} }

// session reads with the read lock of the store. An update has the write lock for all of its work.
type session struct{ s *Store }

func (v session) Update(fn func(tx ops.Tx)) {
	if v.s.device != nil { // the order is always the device store, then the output store
		v.s.device.mu.RLock()
		defer v.s.device.mu.RUnlock()
	}
	v.s.mu.Lock()
	defer v.s.mu.Unlock()
	work := tx{view{v.s, v.s.st.clone()}}
	fn(work)
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
		snapshots: copyMap(st.snapshots), blobs: copyMap(st.blobs), deferred: copyMap(st.deferred),
		heads: copyMap(st.heads), sections: copyMap(st.sections), gates: copyMap(st.gates),
		candidates: copyMap(st.candidates), published: copyMap(st.published), log: st.log[:len(st.log):len(st.log)],
		contexts: copyMap(st.contexts), members: copyMap(st.members), producer: copyMap(st.producer),
		removals: copyMap(st.removals), entries: copyMap(st.entries),
	}
}

// tx is one update. It changes a copy of the state.
type tx struct{ view }

func (t tx) LockCollections([]string) {} // an update has the write lock of the store

func (t tx) LockDigests([]string) {}

func (t tx) PutObject(o ops.Object) {
	if _, _, ok := t.Object(o.Ref); ok {
		return // the store has the descriptor, or its device store has it (IDN-CAN-3, BUN-IMP-8)
	}
	t.st.objects[o.Ref] = object{o.Kind, append([]byte(nil), o.Text...)}
	t.st.targets[o.Ref] = append([]string(nil), o.Targets...)
	sort.Strings(t.st.targets[o.Ref])
	for _, target := range o.Targets {
		set := copyMap(t.st.sources[target])
		set[o.Ref] = struct{}{}
		t.st.sources[target] = set
	}
	if o.Kind == "snapshot" {
		t.st.snapshots[o.Collection]++
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

func (t tx) PutContext(id string, record ops.ContextRecord, members []string) {
	set := make(map[string]struct{}, len(members))
	for _, m := range members {
		set[m] = struct{}{}
	}
	t.st.contexts[id] = record
	t.st.members[id] = set
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

// Fingerprint returns a digest of the content, the catalog and the producer records of the store. It does
// not include the read contexts, the gates or the sections. Two equal stores have equal fingerprints.
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
	sort.Strings(lines)
	h := sha256.New()
	for _, line := range lines {
		h.Write([]byte(line))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}
