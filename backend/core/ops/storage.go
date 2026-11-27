package ops

import (
	"context"
	"errors"
)

// Fault is a failure of the storage, for example a lost connection. A storage reports it with Raise. The
// dispatch changes it into `storage_error`, or into `deadline_exceeded` for a deadline (STS-MAL-1).
type Fault struct{ Err error }

func (f *Fault) Error() string { return "storage fault: " + f.Err.Error() }

func (f *Fault) Unwrap() error { return f.Err }

// Raise stops the operation with a storage fault. It does not return.
//
// The methods of Reader, Tx and Session have no error result. Each operation makes many storage calls, and
// each failure has the same outcome: the call ends, an open update does not commit, and the envelope has
// `storage_error`. Thus a storage calls Raise, and only the dispatch of Store.Call recovers the panic.
func Raise(err error) { panic(&Fault{Err: err}) }

// ContextRecord is the record of one read context (CTX-OPN-2). The record of a layered context has two
// roots: its base snapshot and then its layer (LYR-OPN-3). No other context has a root of kind `layer`.
// Thus the storage keeps the closure of the base snapshot and the closure of the layer for it, and
// ReadContext removes the objects that are not members (LYR-PRE-2).
type ContextRecord struct {
	Roots        []string // the resolved roots, in the order of the request
	Capabilities []string // in ascending order (CTX-PRJ-2)
	// Expires is the end of the declared life of the context, in milliseconds from 1970-01-01T00:00:00Z
	// (CTX-LIF-1, DEV-LAY-5). Zero is a context with no declared life.
	Expires int64
}

// Object is one descriptor that an update writes.
type Object struct {
	Ref        string
	Kind       string
	Text       []byte   // the canonical form (IDN-CAN-1)
	Targets    []string // its `refs` (KND-DEP-2)
	Collection string   // the collection of a snapshot, the base collection of a layer, and "" for each other kind
}

// Candidate is one candidate record of the catalog (PUB-CND-3).
type Candidate struct {
	ID           string
	Snapshot     string
	Collection   string
	Policy       string
	ExpectedHead string // empty if the candidate expects a collection with no head
}

// Publication is one record of a head update (PUB-LOG-2).
type Publication struct {
	Candidate    string
	Receipt      string
	Activation   string // empty if the request had no activation
	Collection   string
	PreviousHead string // empty if the collection had no head
	Head         string
}

// Removal is one removal record of a producer (PRD-RMV-1).
type Removal struct {
	Sequence int64
	Key      string
	Time     string
	Reason   string
}

// RemovalRecord is one removal record of the Evidence section (REL-RMV-5). It has no other field of the
// removed descriptor, no payload digest and no bytes.
type RemovalRecord struct {
	Sequence int64
	Ref      string
	Kind     string
	Time     string
	Reason   string
}

// Listed is one object that a store keeps: its reference and its kind.
type Listed struct {
	Ref  string
	Kind string
}

// Entry is one entry of a producer log (PRD-LOG-1).
type Entry struct {
	Sequence int64
	Data     []byte
}

// Reader reads one store. A local output store also gives the objects, the edges, the bytes and the
// deferred records of its device store (BUN-IMP-8). A result is a copy or is immutable: the caller does
// not change it. A string argument is a JSON string value, so it can have each code point, also U+0000.
type Reader interface {
	// Object returns the kind and the canonical text of a descriptor.
	Object(ref string) (kind string, text []byte, ok bool)
	// Targets returns the `refs` of an object (KND-DEP-2), in ascending order.
	Targets(ref string) []string
	// Sources returns the objects whose `refs` have the object, in ascending order.
	Sources(ref string) []string
	// Blob returns the bytes of a digest with no deferred check. An operation reads bytes with Call.Blob.
	Blob(digest string) ([]byte, bool)
	// HasDeferred tells if a deferred record names the descriptor (BUN-IMP-12).
	HasDeferred(ref string) bool
	// DeferredRefs returns the descriptors with a deferred check that needs the digest, in ascending
	// order (BUN-IMP-10).
	DeferredRefs(digest string) []string
	// HasSnapshot tells if the store has a snapshot of the collection (PUB-SEC-2).
	HasSnapshot(collection string) bool
	// HasLayer tells if the store has a layer whose base collection is the collection (LYR-DEF-5).
	HasLayer(collection string) bool

	// Heads returns the head of each name that has one, from one atomic read (CTX-VIW-3).
	Heads(collections []string) map[string]string
	// Section returns the declared section of a collection (PUB-SEC-1).
	Section(collection string) (string, bool)
	// Gated tells if the collection is a gated collection (PUB-GAT-1).
	Gated(collection string) bool
	// Candidate returns a candidate record.
	Candidate(id string) (Candidate, bool)
	// Published tells if the store has a publication record of the candidate (PUB-HED-4).
	Published(candidate string) bool
	// Publications returns the publication records of a collection, in the order of the head updates (PUB-LOG-3).
	Publications(collection string) []Publication

	// Context returns the record of a read context. A context that the store does not keep gives false.
	Context(id string) (ContextRecord, bool)
	// IsMember tells if an object is in the content of a read context (CTX-OPN-3).
	IsMember(context, ref string) bool
	// Members returns the content of a read context, in ascending order.
	Members(context string) []string

	// ContextsWith returns the identity of each read context that has the object as a member, in ascending
	// order (CTX-LIF-4).
	ContextsWith(ref string) []string
	// OpenRoots returns each root of each read context whose declared life has no end or ends after `now`,
	// in ascending order with no duplicate. `now` is in milliseconds from the Unix epoch. Each root of a
	// context is a member, and each member is in the closure of a root. Thus the closure of these roots is
	// the closure of the members of the open contexts (CTX-LIF-6).
	OpenRoots(now int64) []string
	// EndedContexts returns the identity of each read context whose declared life ends at `now` or before
	// it, in ascending order (CTX-LIF-2). The store does not use such a context again (CTX-LIF-5).
	EndedContexts(now int64) []string

	// Removals returns the removal records of the store, in the order of their sequence (REL-RMV-6). Only a
	// writer store has removal records (REL-RMV-1).
	Removals() []RemovalRecord
	// HasRemoval tells if a removal record has the reference (REL-RMV-3).
	HasRemoval(ref string) bool
	// RemovedRefs returns each reference that a removal record has and that names no object of the store,
	// in ascending order (REL-RMV-7).
	RemovedRefs() []string

	// OwnObjects returns each object that this store keeps, in ascending order of the reference. A local
	// output store does not give the objects of its device store (BUN-RMV-2).
	OwnObjects() []Listed
	// OwnBlobs returns each digest whose bytes this store keeps, in ascending order. A local output store
	// does not give the bytes of its device store (BUN-RMV-2).
	OwnBlobs() []string
	// Naming returns each object that this store keeps whose canonical text has the text of the digest, in
	// ascending order. Each descriptor that names the digest as a payload is in the result. The result can
	// have more objects, so the caller reads the payload fields of each one (REL-RMV-4).
	Naming(digest string) []string
	// AllHeads returns the head of each collection that has one (LIF-GC-2).
	AllHeads() map[string]string
	// Candidates returns each candidate record of the catalog, in ascending order of the identity (LIF-GC-2).
	Candidates() []Candidate

	// ProducerObject returns the bytes of a producer key (PRD-OBJ-3).
	ProducerObject(producer, key string) ([]byte, bool)
	// ProducerRemoved tells if the producer has a removal record of the key (PRD-RMV-2).
	ProducerRemoved(producer, key string) bool
	// ProducerRemovals returns the removal records of a producer, in the order of their sequence (PRD-RMV-4).
	ProducerRemovals(producer string) []Removal
	// ProducerEntries returns the entries of a producer log, in the order of their sequence (PRD-LOG-3).
	ProducerEntries(producer, log string) []Entry
}

// Tx is one update of a store. It reads the store with the writes of this update. Each write of content
// adds only: a descriptor or bytes that the store has do not change (IDN-CAN-3). A local output store
// ignores a descriptor or bytes that its device store has (BUN-IMP-8). Only the removal methods take
// content out of the store, and only `remove` and `gc_collect` call them (REL-RMV-4, LIF-GC-5). A removal
// method changes only the store of the update, never its device store. An update of a Tx with RemovalGate
// calls LockRemoval before its first removal method.
type Tx interface {
	Reader
	// LockCollections makes this update wait for each other update that locked one of the collections,
	// until it ends. A read of a head or a section after the lock is stable (PUB-HED-3, PUB-SEC-2).
	LockCollections(collections []string)
	// LockDigests does the same for the bytes and the deferred records of the digests (BUN-IMP-10).
	LockDigests(digests []string)

	PutObject(o Object)
	PutBlob(digest string, data []byte)
	// ClearDeferred removes each deferred record of the digest (BUN-IMP-10).
	ClearDeferred(digest string)
	PutDeferred(digest, ref string)

	DeclareGate(collection string)
	DeclareSection(collection, section string)
	PutCandidate(c Candidate)
	// Publish sets the head of the collection of the record and adds the record (PUB-HED-3 step 5).
	Publish(p Publication)

	// PutContext adds a read context. Its content is the closure of the roots of the record, without each
	// removed object: a reference of RemovedRefs at the time of the call (CTX-OPN-2, KND-DEP-3, REL-RMV-8).
	// A later write of a removed descriptor does not add the object to the context (REL-RMV-11). Each root
	// is a stored object. A removal keeps the `refs` of the removed object, so the references of the closure
	// of a root do not change, and a storage can keep them from the first use of the root.
	PutContext(id string, record ContextRecord)
	// DeleteContexts ends each read context of the list: the store does not keep it (CTX-LIF-2).
	DeleteContexts(ids []string)

	// DeleteObjects removes the descriptor of each reference of the list. The `refs` of the object stay
	// (REL-RMV-8).
	DeleteObjects(refs []string)
	// DeleteEdges removes the `refs` of each reference of the list, and each stored closure that has the
	// reference as its root or as a member (LIF-GC-5).
	DeleteEdges(sources []string)
	// DeleteDeferred removes each deferred record of each descriptor of the list (REL-RMV-4, LIF-GC-5).
	DeleteDeferred(refs []string)
	// DeleteBlobs removes the bytes of each digest of the list, with the derived data of the bytes: the
	// text index and the row cache (REL-RMV-4, LIF-GC-5).
	DeleteBlobs(digests []string)
	// DeleteCandidates removes each candidate record of the list (LIF-GC-5).
	DeleteCandidates(ids []string)
	// PutRemoval adds one removal record (REL-RMV-5).
	PutRemoval(r RemovalRecord)

	PutProducerObject(producer, key string, data []byte)
	// DeleteProducerObject removes the bytes of a key so that no copy stays in the store (PRD-RMV-3).
	DeleteProducerObject(producer, key string)
	PutProducerRemoval(producer string, r Removal)
	PutProducerEntry(producer, log string, e Entry)
}

// Session is a store for one call.
type Session interface {
	Reader
	// Update runs fn as one atomic update: the store gets each write of fn, or none (STS-MAL-2). The
	// update commits when fn returns. If fn panics, the update does not commit and the panic continues.
	Update(fn func(tx Tx))
}

// Storage is one store: a writer store, a device store or a local output store.
type Storage interface {
	// Session returns the store for one call. The context gives the deadline of the call.
	Session(ctx context.Context) Session
}

// ProducerLock is an optional part of a Tx of a writer store. A storage whose updates run at the same time
// has it. A storage that runs one update at a time does not need it.
type ProducerLock interface {
	// LockProducer makes this update wait for each other update that locked the producer, until it ends.
	// A read of the objects, the logs and the removal records of the producer after the lock is stable
	// (PRD-OBJ-2, PRD-LOG-2, PRD-RMV-1).
	LockProducer(producer string)
}

// ProducerCounts is an optional part of a Reader: the number of entries of a log and the number of removal
// records of a producer, with no read of the bytes. Without it, an operation counts the result of
// ProducerEntries and ProducerRemovals.
type ProducerCounts interface {
	ProducerEntryCount(producer, log string) int64
	ProducerRemovalCount(producer string) int64
}

// ClosureIndex is an optional part of a Reader: the closure of one root in one read (KND-DEP-3). Without
// it, an operation walks the `refs` with Targets.
type ClosureIndex interface {
	// Closure returns the root and each object that its references reach, in ascending order. The storage
	// reads the stored closure of the root, or it makes the closure from the `refs` in one read.
	Closure(root string) []string
}

// BatchReader is an optional part of a Reader: reads for a list in one read. Without it, an operation
// makes one read for each entry.
type BatchReader interface {
	// Objects returns the canonical text of each descriptor of the list that the store has.
	Objects(refs []string) map[string][]byte
	// Deferred returns each descriptor of the list that a deferred record names (BUN-IMP-12).
	Deferred(refs []string) map[string]bool
	// Blobs returns the bytes of each digest of the list that the store has, with no deferred check.
	Blobs(digests []string) map[string][]byte
}

// TextIndex is an optional part of a Tx and of a Reader: an index of the words of text payloads
// (CRD-TXT-3). The index gives candidates only. The matches always come from the bytes.
//
// The store has the bytes of each digest that the index has: PutTextTokens comes only with the bytes, and
// DeleteBlobs removes the tokens of a digest with its bytes.
type TextIndex interface {
	// PutTextTokens records the word tokens of the bytes of a digest, in the order of the words.
	// A second call for one digest has no effect.
	PutTextTokens(digest string, tokens []string)
	// TextCandidates reads the index for the digests of a list. `candidates` has each digest of the list
	// that the index has and that can have the tokens as consecutive words. It can have more digests of
	// the list. `unindexed` has each digest of the list that the index does not have.
	TextCandidates(tokens, digests []string) (candidates, unindexed []string)
	// TextIndexed tells if the index has the tokens of the digest.
	TextIndexed(digest string) bool
}

// ImportLog is an optional part of a Tx of a device store: the record of each `ok` import (DEV-UPD-2). The
// record is not content of the store, and only `gc_collect` reads it (BUN-RMV-1).
type ImportLog interface {
	// PutImport adds the record of one import, with the roots of the bundle in the order of the bundle.
	PutImport(roots []string)
	// ImportRoots returns each root of each recorded import, in ascending order with no duplicate.
	ImportRoots() []string
}

// OutputView is an optional part of a Tx of a device store: a reader of its local output store that does
// not change until the update ends. `gc_collect` reads the retained roots of the local output store from
// it (BUN-RMV-1). Without it, the operation reads the local output store through a session of its own.
type OutputView interface {
	// Output returns the reader, or false for a device store with no local output store.
	Output() (Reader, bool)
}

// RemovalEpoch is an optional part of a Session of a storage whose updates run at the same time, in one
// process or in more than one. The epoch is a number that each update with AdvanceEpoch increments. A
// store reads it at the start of each call. If it changed, an update removed content, and the store drops
// its parsed descriptors.
type RemovalEpoch interface {
	RemovalEpoch() int64
}

// RemovalGate is an optional part of a Tx of a storage whose updates run at the same time.
type RemovalGate interface {
	// LockRemoval makes this update wait for each other update that wrote, until it ends, and makes each
	// later update wait for this one before its first write. `remove` and `gc_collect` call it first.
	LockRemoval()
	// AdvanceEpoch increments the removal epoch in this update. An update of a different call that saw
	// the old epoch stops with ErrRemoved before its first write.
	AdvanceEpoch()
}

// ErrRemoved is the storage fault of an update whose call started before a removal that committed. The
// checks of the call read content that the store can have removed. Thus the update does not write, and
// the dispatch runs the call again (REL-RMV-4, LIF-GC-5).
var ErrRemoved = errors.New("a removal committed after the start of the call")

// RowCache is an optional part of a Tx and of a Reader: the declared columns and the rows of a table
// payload that agrees with DAT-TBL-1 (DAT-TBL-3), so that a read of one row does not parse the payload
// again. A row is the canonical text of the list of its values (RFC 8785), in the order of the columns.
//
// The store has the bytes of each digest that the cache has: PutTable comes only with the bytes, and
// DeleteBlobs removes the table of a digest with its bytes.
type RowCache interface {
	// PutTable records the columns and the rows of a table, also of a table with no row. A second call
	// for one digest has no effect.
	PutTable(digest string, columns []string, rows [][]byte)
	// TableHead returns the declared columns and the number of rows of a table. A digest that the cache
	// does not have gives false.
	TableHead(digest string) (columns []string, count int, ok bool)
	// TableRow returns one row of a table.
	TableRow(digest string, index int) ([]byte, bool)
	// TableRows returns each row of a table, in the table order.
	TableRows(digest string) [][]byte
}
