// Package ops has the dispatch of the operation table (document 03), the storage interface, and the
// helpers that the operations share. The packages read, text, transfer and write add the operations to a
// Registry. Package ops has no operation of its own.
//
// An operation ends in one of two ways. It returns its envelope, or it stops with a panic that only the
// dispatch recovers: Fail for an outcome of the operation as a whole (STS-ENV-3 rule 1), and Raise for a
// storage fault. No panic and no storage message crosses Store.Call (STS-MAL-1).
package ops

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mohanraj00/aavanam/backend/core/envelope"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/kinds"
)

// Role is the role of a store (BUN-IMP-5).
type Role string

// The roles of a store.
const (
	Writer Role = "writer"
	Device Role = "device"
	Output Role = "output"
)

// Sections has the names of the storage sections of a collection (PUB-SEC-1).
var Sections = []string{"serving", "evidence"}

// writes has the operations that a device store refuses (BUN-IMP-6).
var writes = map[string]bool{"put": true, "create_snapshot": true, "create_candidate": true, "publish": true}

// storeReads has the operations that read the store and its catalog, not a read context (LIF-ITM-4).
var storeReads = map[string]bool{"snapshot_changes": true, "item_history": true}

// needs has the capability that an operation needs (CTX-PRJ-1, IDX-PRF-4). An operation of a profile is
// available only in a store that declares the profile.
var needs = func() map[string]string {
	out := map[string]string{}
	tables := []map[string][]string{Capabilities}
	for _, table := range ProfileCapabilities {
		tables = append(tables, table)
	}
	for _, table := range tables {
		for capability, operations := range table {
			for _, operation := range operations {
				out[operation] = capability
			}
		}
	}
	return out
}()

// Known tells if the operation table has the operation as an operation of the base profile.
func Known(operation string) bool {
	_, ok := table[operation]
	return ok
}

// InReadBinding tells if the operation is an operation of the network read binding (NET-SCP-1).
func InReadBinding(operation string) bool { return readBinding[operation] }

// Permits tells if the row of the operation has the status `partial` (STS-ENV-3). The row can be a row of
// a profile (IDX-PRF-4).
func Permits(operation string) bool {
	if r, ok := profileTable[operation]; ok {
		return r.partial
	}
	return table[operation].partial
}

// Writes tells if a device store refuses the operation with `read_only_store` (BUN-IMP-6).
func Writes(operation string) bool { return writes[operation] }

// Operations returns the operations of the base profile in ascending order.
func Operations() []string {
	out := make([]string, 0, len(table))
	for operation := range table {
		out = append(out, operation)
	}
	sort.Strings(out)
	return out
}

// Handler is one operation. The request of the call is a JSON object.
type Handler func(c *Call) envelope.Envelope

// Indexer gets each descriptor and payload whose bytes are in the store after an update, in the update.
// It writes derived data, for example the tokens of a text payload (CRD-TXT-2).
type Indexer func(tx Tx, desc jsontext.Value, digest string, data []byte)

// Registry has the operations of a store.
type Registry struct {
	handlers map[string]Handler
	indexers []Indexer
}

// NewRegistry returns a registry with no operation.
func NewRegistry() *Registry { return &Registry{handlers: map[string]Handler{}} }

// Handle adds an operation. The operation has a row in the operation table or in the operation table of a
// profile, and only one handler. A store runs an operation of a profile only if it declares the profile.
func (r *Registry) Handle(operation string, h Handler) {
	_, base := table[operation]
	if _, profile := profileTable[operation]; !base && !profile {
		panic("ops: the operation table has no operation " + operation)
	}
	if _, ok := r.handlers[operation]; ok {
		panic("ops: the operation " + operation + " has two handlers")
	}
	r.handlers[operation] = h
}

// OnBytes adds an indexer.
func (r *Registry) OnBytes(f Indexer) { r.indexers = append(r.indexers, f) }

// Indexers returns the indexers in the order of their registration.
func (r *Registry) Indexers() []Indexer { return r.indexers }

// Decision is the answer of a validator.
type Decision int

// The answers of a validator. NoAnswer is a validator that did not answer before its deadline (PUB-RCP-4).
const (
	Rejected Decision = iota
	Accepted
	NoAnswer
)

// Validator is the validator of an acceptance policy of the application (PUB-RCP-1). The activation is
// empty if the request has none (PUB-RCP-2). The store never runs a stored artifact: a validator is code
// of the host application.
type Validator func(ctx context.Context, candidate, receipt, activation string) Decision

// cacheLimit is the number of parsed descriptors that a store keeps.
const cacheLimit = 1 << 16

// memoLimit is the number of references that the values of ReadContext.Memo keep, for all contexts.
const memoLimit = 1 << 19

// memoKey names one value of ReadContext.Memo.
type memoKey struct{ context, name string }

// Store is one store with one role and its operations.
type Store struct {
	Role    Role
	Storage Storage
	// Output is the local output store of a device store. An operation on the device store writes a lock
	// there (BUN-IMP-7). It is nil for each other role.
	Output *Store
	// Debug lets a panic that is not a failure or a storage fault continue, for tests.
	Debug bool
	// Clock is the store clock, for the time of a removal record (PRD-RMV-1). Nil is the system clock.
	Clock func() time.Time
	// Profiles has the profiles that the store declares separately from the base profile (LYR-PRF-1,
	// IDX-PRF-1). The application sets it before the first call, and it does not change after that. A nil
	// set is a store of the base profile only.
	Profiles kinds.Profiles

	registry *Registry
	called   atomic.Bool // a gate or a section is declared before the first call (PUB-GAT-1, PUB-SEC-1)
	mu       sync.RWMutex
	policies map[string]Validator
	cache    map[string]jsontext.Value // descriptors do not change (IDN-CAN-1)
	memo     map[memoKey]any           // the content of a context does not change (CTX-OPN-2)
	memoCost int
}

// New returns a store. The registry does not change after this call.
func New(role Role, storage Storage, registry *Registry) *Store {
	return &Store{Role: role, Storage: storage, registry: registry, policies: map[string]Validator{},
		cache: map[string]jsontext.Value{}, memo: map[memoKey]any{}}
}

// Accept registers the validator of an acceptance policy (PUB-RCP-1).
func (s *Store) Accept(policy string, validate Validator) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.policies[policy] = validate
}

// Policy returns the validator of a policy.
func (s *Store) Policy(policy string) (Validator, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.policies[policy]
	return v, ok
}

// Now returns the UTC time of the store clock in the form `YYYY-MM-DDTHH:MM:SSZ` (PRD-RMV-1).
func (s *Store) Now() string {
	now := time.Now
	if s.Clock != nil {
		now = s.Clock
	}
	return now().UTC().Format("2006-01-02T15:04:05Z")
}

// Supports tells if the operation has a row with more than `unsupported` for this store: a row of the
// operation table of document 03, or a row of a profile that the store declares (IDX-PRF-4, RSV-LST-4).
func (s *Store) Supports(operation string) bool {
	if r, ok := profileTable[operation]; ok && s.Profiles[r.profile] {
		return true
	}
	return Known(operation)
}

// Capabilities returns the capability table of the store: the table of CTX-PRJ-1 and the rows of each
// profile that the store declares (IDX-PRF-4). The caller does not change the result.
func (s *Store) Capabilities() map[string][]string {
	if len(s.Profiles) == 0 {
		return Capabilities
	}
	out := make(map[string][]string, len(Capabilities)+1)
	for capability, operations := range Capabilities {
		out[capability] = operations
	}
	for profile := range s.Profiles {
		for capability, operations := range ProfileCapabilities[profile] {
			out[capability] = operations
		}
	}
	return out
}

// Handles tells if the store has the operation.
func (s *Store) Handles(operation string) bool {
	_, ok := s.registry.handlers[operation]
	return ok
}

// Stopped returns the envelope of a call that did not complete, with one reason: the status of the code, no
// items, and the context of the request if the request names one (STS-ENV-3 rule 1). A binding uses it for
// a call that its deadline stopped (NET-LIM-2).
func (s *Store) Stopped(operation string, request []byte, code string) envelope.Envelope {
	failure := envelope.Fail(code)
	return envelope.Envelope{Status: failure.Status, Reasons: failure.Reasons, Operation: jsontext.S(operation),
		Context: echoOf(operation, request), HasItems: true, Fields: empty(operation)}
}

// echoOf returns the context of a request, if the request names one, for the envelope of a failure
// (LIF-ITM-4). A store read has no read context.
func echoOf(operation string, request []byte) jsontext.Value {
	value, formError := jsontext.Parse(request, jsontext.Lenient)
	if formError != nil {
		return jsontext.Value{}
	}
	return echo(operation, value)
}

func echo(operation string, request jsontext.Value) jsontext.Value {
	if id, ok := request.Get("context"); ok && id.Kind == jsontext.String && !storeReads[operation] {
		return id
	}
	return jsontext.Value{}
}

// Indexers returns the indexers of the registry of the store.
func (s *Store) Indexers() []Indexer { return s.registry.indexers }

// refused is the panic of a declaration that the store does not accept.
type refused struct{ err error }

// configure runs one declaration as one update. A refused declaration or a storage fault gives an error,
// and the update does not commit.
func (s *Store) configure(ctx context.Context, fn func(tx Tx) error) (err error) {
	defer func() {
		switch p := recover().(type) {
		case nil:
		case refused:
			err = p.err
		case *Fault:
			err = p
		default:
			panic(p)
		}
	}()
	s.Storage.Session(ctx).Update(func(tx Tx) {
		if e := fn(tx); e != nil {
			panic(refused{e})
		}
	})
	return nil
}

// Gate declares a gated collection (PUB-GAT-1). The store keeps the declaration for its life (PUB-GAT-2).
// The declaration is configuration, so it comes before the first call.
func (s *Store) Gate(ctx context.Context, collection string) error {
	if s.called.Load() {
		return errors.New("declare a gated collection before the first call (PUB-GAT-1)")
	}
	return s.configure(ctx, func(tx Tx) error {
		// The atomic step of a `publish` has this lock when it applies the gate check (PUB-GAT-3). Thus a
		// declaration ends before that step starts or after it ends, never between its check and its commit.
		tx.LockCollections([]string{collection})
		tx.DeclareGate(collection)
		return nil
	})
}

// Section declares the storage section of a collection (PUB-SEC-1). The store keeps it for its life. The
// declaration fails, and the store does not change, if the collection has a different section or a
// snapshot (PUB-SEC-2), or if it is the base collection of a layer (LYR-DEF-5).
func (s *Store) Section(ctx context.Context, collection, section string) error {
	switch {
	case section != Sections[0] && section != Sections[1]:
		return fmt.Errorf("%q is not a section name (PUB-SEC-1)", section)
	case s.Role != Writer:
		return errors.New("only a writer store has sections (PUB-SEC-1)")
	case s.called.Load():
		return errors.New("declare a section before the first call (PUB-SEC-1)")
	}
	return s.configure(ctx, func(tx Tx) error {
		tx.LockCollections([]string{collection}) // atomic with a snapshot write of a different open (PUB-SEC-2)
		declared, has := tx.Section(collection)
		switch {
		case has && declared != section:
			return fmt.Errorf("the collection already has the section %s (PUB-SEC-2)", declared)
		case has:
			return nil
		case tx.HasSnapshot(collection):
			return errors.New("the collection already has a snapshot (PUB-SEC-2)")
		case tx.HasLayer(collection):
			return errors.New("the collection is already the base of a layer (LYR-DEF-5)")
		}
		tx.DeclareSection(collection, section)
		return nil
	})
}

// empty returns the added fields of a result that did not complete (STS-ENV-3 rule 1, RND-DOC-3).
func empty(operation string) []jsontext.Member {
	if operation != "render" {
		return nil
	}
	return []jsontext.Member{jsontext.M("document", jsontext.Value{}), jsontext.M("document_id", jsontext.Value{})}
}

// Call returns the envelope of one call. The operation is the JSON value that names it. The request is
// JSON text that no decoder changed (NET-TRN-2). The order of the checks is the order of STS-MAL-4.
func (s *Store) Call(ctx context.Context, operation jsontext.Value, request []byte) (out envelope.Envelope) {
	name, isName := operation.Text()
	if !isName {
		return envelope.New(jsontext.Value{}, "unsupported", envelope.R("unsupported_operation"))
	}
	if profile, reserved := Reserved[name]; reserved && !s.Supports(name) { // RSV-RSP-1, RSV-LST-4
		return envelope.New(operation, "unsupported", envelope.Reason{Code: "reserved_profile", Capabilities: []string{profile}})
	}
	handler := s.registry.handlers[name]
	if !s.Supports(name) {
		return envelope.New(operation, "unsupported", envelope.R("unsupported_operation"))
	}
	s.called.Store(true)
	// The envelope of a failure has the context of the request, if the request names one (LIF-ITM-4).
	value, formError := jsontext.Parse(request, jsontext.Lenient)
	context := jsontext.Value{}
	if formError == nil {
		context = echo(name, value)
	}
	defer func() {
		p := recover()
		if p == nil {
			return
		}
		var failure *envelope.Failure
		switch v := p.(type) {
		case *envelope.Failure:
			failure = v
		case *Fault:
			failure = FaultFailure(v)
		default:
			if s.Debug {
				panic(p)
			}
			failure = envelope.Fail("storage_error") // STS-MAL-1: no raw failure crosses the API
		}
		out = envelope.Envelope{Status: failure.Status, Reasons: failure.Reasons, Operation: operation, Context: context,
			HasItems: true, Fields: empty(name)}
	}()
	switch {
	case s.Role == Device && writes[name]:
		Fail("read_only_store") // BUN-IMP-6: after STS-MAL-4, before the request
	case strings.HasPrefix(name, "producer_") && s.Role != Writer:
		Fail("producer_section_absent") // PRD-SEC-1: after STS-MAL-4, before the request
	case handler == nil:
		Fail("unsupported_operation") // a store that does not have each operation of the base profile
	}
	if formError != nil {
		Fail(formError.Codes...) // NET-TRN-2
	}
	if value.Kind != jsontext.Object {
		Fail("field_type")
	}
	return handler(&Call{Ctx: ctx, Store: s, S: s.Storage.Session(ctx), Operation: name, Request: value})
}

// FaultFailure returns the failure of a storage fault: `deadline_exceeded` for a deadline, and
// `storage_error` for each other fault. The message of the storage does not cross the API (STS-MAL-1).
func FaultFailure(f *Fault) *envelope.Failure {
	if errors.Is(f.Err, context.DeadlineExceeded) {
		return envelope.Fail("deadline_exceeded")
	}
	return envelope.Fail("storage_error")
}

// Fail stops the operation with a failure of the operation as a whole: one reason for each code, and the
// status of the first code (STS-ENV-3 rule 1). It does not return.
func Fail(codes ...string) { panic(envelope.Fail(codes...)) }

// FailWith stops the operation with the given status and reasons. It does not return.
func FailWith(status string, reasons ...envelope.Reason) {
	panic(&envelope.Failure{Status: status, Reasons: reasons})
}
