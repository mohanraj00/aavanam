package ops

import (
	"context"
	"sort"

	"github.com/mohanraj00/aavanam/backend/core/check"
	"github.com/mohanraj00/aavanam/backend/core/envelope"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
)

// Call is one call of an operation.
type Call struct {
	Ctx       context.Context
	Store     *Store
	S         Session
	Operation string
	Request   jsontext.Value // a JSON object from the Lenient reader: the operation checks each field
}

// On returns a call of the same operation and request on a different store, for example on the local
// output store of a device store (BUN-IMP-7).
func (c *Call) On(store *Store) *Call {
	return &Call{Ctx: c.Ctx, Store: store, S: store.Storage.Session(c.Ctx), Operation: c.Operation, Request: c.Request}
}

// parse returns the value of a stored descriptor. The store wrote the text, so it is canonical.
func parse(text []byte) jsontext.Value {
	value, err := jsontext.Parse(text, jsontext.Strict)
	if err != nil {
		Raise(err)
	}
	return value
}

// DescriptorIn returns the normal descriptor of a reference from a reader, for example from an update.
func DescriptorIn(r Reader, ref string) (jsontext.Value, bool) {
	_, text, ok := r.Object(ref)
	if !ok {
		return jsontext.Value{}, false
	}
	return parse(text), true
}

// Descriptor returns the normal descriptor of a reference in the store. The caller does not change it.
func (c *Call) Descriptor(ref string) (jsontext.Value, bool) {
	s := c.Store
	s.mu.RLock()
	value, ok := s.cache[ref]
	s.mu.RUnlock()
	if ok {
		return value, true
	}
	if value, ok = DescriptorIn(c.S, ref); !ok {
		return jsontext.Value{}, false
	}
	s.mu.Lock()
	if len(s.cache) >= cacheLimit {
		s.cache = map[string]jsontext.Value{}
	}
	s.cache[ref] = value
	s.mu.Unlock()
	return value, true
}

// Kind returns the kind of an object in the store. An object that is not in the store gives "".
func (c *Call) Kind(ref string) string {
	desc, ok := c.Descriptor(ref)
	if !ok {
		return ""
	}
	return check.Kind(desc)
}

// Env returns the view of the store for the checks of package check.
func (c *Call) Env() check.Env {
	return check.Env{Lookup: c.Descriptor, Data: c.S.Blob}
}

// Blob returns the bytes of a payload of a descriptor. If the descriptor has a deferred record, the bytes
// are available only after its deferred checks pass (BUN-IMP-12). Bytes that fail a check, or whose check
// is not complete, use the read rules of absent bytes.
func (c *Call) Blob(digest, ref string) ([]byte, bool) {
	data, ok := c.S.Blob(digest)
	if !ok || !c.S.HasDeferred(ref) {
		return data, ok
	}
	desc, _ := c.Descriptor(ref)
	env := c.Env()
	if env.Payloads(desc) != nil || env.Content(desc) != nil {
		return nil, false
	}
	return data, true
}

// ClosureIn returns the roots and each object that their references reach (KND-DEP-3), from a reader.
func ClosureIn(r Reader, roots []string) map[string]struct{} {
	seen := make(map[string]struct{}, len(roots))
	todo := append([]string(nil), roots...)
	for _, root := range roots {
		seen[root] = struct{}{}
	}
	for len(todo) > 0 {
		ref := todo[len(todo)-1]
		todo = todo[:len(todo)-1]
		for _, target := range r.Targets(ref) {
			if _, ok := seen[target]; !ok {
				seen[target] = struct{}{}
				todo = append(todo, target)
			}
		}
	}
	return seen
}

// Closure returns the roots and each object that their references reach (KND-DEP-3).
func (c *Call) Closure(roots ...string) map[string]struct{} { return ClosureIn(c.S, roots) }

// Sorted returns the members of a set in ascending order.
func Sorted(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for member := range set {
		out = append(out, member)
	}
	sort.Strings(out)
	return out
}

// ReadContext is the read context of a call (CTX-OPN-3).
type ReadContext struct {
	ID           string
	Roots        []string
	Capabilities []string
	call         *Call
}

// Has tells if an object is in the content of the read context.
func (rc *ReadContext) Has(ref string) bool { return rc.call.S.IsMember(rc.ID, ref) }

// Members returns the content of the read context, in ascending order.
func (rc *ReadContext) Members() []string { return rc.call.S.Members(rc.ID) }

// Locate returns the reason code of a reference that is not in the read context, or "" (QRY-GET-2, CTX-OPN-4).
func (rc *ReadContext) Locate(ref string) string {
	if rc.Has(ref) {
		return ""
	}
	if _, ok := rc.call.Descriptor(ref); ok {
		return "not_in_context"
	}
	return "unknown_ref"
}

// Context returns the read context that the request names (CTX-OPN-3). A field `context` that is not a
// string gives `field_type`. A context that the store does not keep gives `context_expired`. If the
// operation of the call needs a capability that the context does not have, the result is
// `capability_unavailable` (CTX-PRJ-4).
func (c *Call) Context() *ReadContext {
	id, _ := c.Request.Get("context")
	if id.Kind != jsontext.String {
		Fail("field_type")
	}
	record, ok := c.S.Context(id.Str)
	if !ok {
		Fail("context_expired")
	}
	if capability, ok := needs[c.Operation]; ok && !contains(record.Capabilities, capability) {
		FailWith("unsupported", envelope.Reason{Code: "capability_unavailable", Capabilities: []string{capability}})
	}
	return &ReadContext{ID: id.Str, Roots: record.Roots, Capabilities: record.Capabilities, call: c}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// Envelope returns an envelope of the operation of the call with an item list.
func (c *Call) Envelope(status string, reasons []envelope.Reason, items []envelope.Item) envelope.Envelope {
	return envelope.Envelope{Status: status, Reasons: reasons, Operation: jsontext.S(c.Operation), HasItems: true, Items: items}
}

// Result returns the envelope of the items of a complete read or write, with the status of rules 4 to 6 of
// STS-ENV-3.
func (c *Call) Result(items []envelope.Item) envelope.Envelope {
	status, reasons := envelope.Summary(items, Permits(c.Operation))
	return c.Envelope(status, reasons, items)
}

// Bounded returns the envelope of the items of an operation that a declared bound stopped (STS-ENV-3 rules
// 2 and 3).
func (c *Call) Bounded(items []envelope.Item) envelope.Envelope {
	ok, bad := 0, 0
	for _, i := range items {
		if i.Status == "ok" {
			ok++
		} else {
			bad++
		}
	}
	if ok == 0 || !Permits(c.Operation) {
		return c.Envelope("failed", []envelope.Reason{{Code: "incomplete_result"}}, nil)
	}
	reasons := []envelope.Reason{{Code: "bounds_reached"}}
	if bad > 0 {
		reasons = append(reasons, envelope.Reason{Code: "items_not_ok"})
	}
	return c.Envelope("partial", reasons, items)
}
