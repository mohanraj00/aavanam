package write

import (
	"sort"

	"github.com/mohanraj00/aavanam/backend/core/check"
	"github.com/mohanraj00/aavanam/backend/core/envelope"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/ops"
)

// The removal of content: `remove` and `removals` (document 06, topic RMV) and `gc_collect` (document 17,
// topic GC, and document 11, topic RMV).

// evidenceKinds has the kinds that `remove` takes from the Evidence section (REL-RMV-3). These are the
// kinds that a `serving` collection does not permit (PUB-SEC-3).
var evidenceKinds = map[string]bool{"source": true, "reading": true, "excerpt": true, "evidence": true}

// lockRemoval makes the update wait for each other update that writes, if the storage runs updates at the
// same time (REL-RMV-4, LIF-GC-5).
func lockRemoval(tx ops.Tx) {
	if gate, has := tx.(ops.RemovalGate); has {
		gate.LockRemoval()
	}
}

// advanceEpoch tells each other open of the storage that this update removed content.
func advanceEpoch(tx ops.Tx) {
	if gate, has := tx.(ops.RemovalGate); has {
		gate.AdvanceEpoch()
	}
}

// names tells if an object that the reader keeps names the digest as a payload. The storage gives each
// descriptor whose text has the digest, and the payload fields give the exact answer (REL-RMV-4).
func names(r ops.Reader, digest string) bool {
	for _, ref := range r.Naming(digest) {
		desc, _ := ops.DescriptorIn(r, ref)
		for _, p := range check.Payloads(desc) {
			if p.Digest == digest {
				return true
			}
		}
	}
	return false
}

func recordItem(r ops.RemovalRecord) envelope.Item {
	return envelope.OK(jsontext.M("sequence", jsontext.N(float64(r.Sequence))), jsontext.M("ref", jsontext.S(r.Ref)),
		jsontext.M("kind", jsontext.S(r.Kind)), jsontext.M("time", jsontext.S(r.Time)), jsontext.M("reason", jsontext.S(r.Reason)))
}

// Remove is the operation `remove`: the removal of one object of the Evidence section and its record, in
// one update (REL-RMV-2 to REL-RMV-5). The `refs` of the object stay in the store (REL-RMV-8).
func Remove(c *ops.Call) envelope.Envelope {
	c.Strict([]ops.Field{ops.Req("ref", ops.IsRef), ops.Req("reason", ops.IsName(1024))}, nil, nil) // REL-RMV-2
	ref := textOf(c.Request, "ref")
	var record ops.RemovalRecord
	c.S.Update(func(tx ops.Tx) {
		lockRemoval(tx)
		kind, text, has := tx.Object(ref)
		if !has { // REL-RMV-3 steps 1 and 2: the dispatch gives `removed` for a removed object (REL-RMV-7)
			ops.FailWith("not_found", envelope.R("unknown_ref", ref))
		}
		if !evidenceKinds[kind] { // step 3
			ops.FailWith("unsupported", envelope.R("unsupported_target_kind", ref))
		}
		desc, err := jsontext.Parse(text, jsontext.Strict)
		if err != nil {
			ops.Raise(err)
		}
		tx.DeleteObjects([]string{ref}) // REL-RMV-4 action 1. The `refs` stay (REL-RMV-8).
		var unnamed []string
		seen := map[string]bool{}
		for _, p := range check.Payloads(desc) { // action 2: only the bytes that no other object names
			if !seen[p.Digest] && !names(tx, p.Digest) {
				unnamed = append(unnamed, p.Digest)
			}
			seen[p.Digest] = true
		}
		sort.Strings(unnamed)
		tx.DeleteBlobs(unnamed)
		tx.DeleteDeferred([]string{ref}) // action 3
		record = ops.RemovalRecord{Sequence: int64(len(tx.Removals())) + 1, Ref: ref, Kind: kind, Time: c.Store.Now(),
			Reason: textOf(c.Request, "reason")} // REL-RMV-5: no other field, no content
		tx.PutRemoval(record)                   // action 4
		tx.DeleteContexts(tx.ContextsWith(ref)) // action 5, CTX-LIF-4
		advanceEpoch(tx)
	})
	c.Forget([]string{ref})
	return c.Envelope("ok", nil, []envelope.Item{recordItem(record)})
}

// Removals is the operation `removals`: the removal records of the store in the order of their sequence
// (REL-RMV-6). It reads the store, not a read context.
func Removals(c *ops.Call) envelope.Envelope {
	c.Strict(nil, nil, nil)
	items := []envelope.Item{}
	for _, r := range c.S.Removals() {
		items = append(items, recordItem(r))
	}
	return c.Envelope("ok", nil, items)
}

// references returns the roots and each reference that their references reach, from a reader (KND-DEP-3).
// The result has the removed objects of the closure also (REL-RMV-8, LIF-GC-2).
func references(r ops.Reader, roots []string) set {
	index, ok := r.(ops.ClosureIndex)
	if !ok {
		return ops.ClosureIn(r, roots)
	}
	seen := make(set, len(roots))
	for _, root := range roots {
		if _, done := seen[root]; done {
			continue
		}
		for _, member := range index.Closure(root) {
			seen[member] = struct{}{}
		}
	}
	return seen
}

// chunk is the number of descriptors of one read of payloadsOf.
const chunk = 1024

// payloadsOf adds each payload digest that an object of the list names to the set.
func payloadsOf(r ops.Reader, refs []string, into set) {
	add := func(text []byte) {
		desc, err := jsontext.Parse(text, jsontext.Strict)
		if err != nil {
			ops.Raise(err)
		}
		for _, p := range check.Payloads(desc) {
			into[p.Digest] = struct{}{}
		}
	}
	batch, ok := r.(ops.BatchReader)
	for start := 0; start < len(refs); start += chunk {
		part := refs[start:min(start+chunk, len(refs))]
		if ok {
			for _, text := range batch.Objects(part) {
				add(text)
			}
			continue
		}
		for _, ref := range part {
			if _, text, has := r.Object(ref); has {
				add(text)
			}
		}
	}
}

// retainedRoots returns the retained roots of `gc_collect`: LIF-GC-2 for a writer store, and rules 1 and 3
// of BUN-RMV-1 for a device store. `own` has the objects that the store keeps.
func retainedRoots(c *ops.Call, tx ops.Tx, own map[string]string) []string {
	requested, named := c.Request.Get("roots")
	roots := ops.Strings(requested)
	roots = append(roots, tx.OpenRoots(c.Store.Millis())...) // CTX-LIF-6
	if c.Store.Role == ops.Device {
		if log, has := tx.(ops.ImportLog); has && !named { // each imported root that the store has
			for _, root := range log.ImportRoots() {
				if _, kept := own[root]; kept {
					roots = append(roots, root)
				}
			}
		}
		return roots
	}
	heads := tx.AllHeads()
	collections := make([]string, 0, len(heads))
	for collection := range heads {
		collections = append(collections, collection)
	}
	sort.Strings(collections)
	for _, collection := range collections {
		roots = append(roots, heads[collection]) // rule 1
		if log := tx.Publications(collection); len(log) > 0 {
			newest := log[len(log)-1] // rule 2
			roots = append(roots, newest.Receipt)
			if newest.Activation != "" {
				roots = append(roots, newest.Activation)
			}
		}
	}
	for _, candidate := range tx.Candidates() {
		if !tx.Published(candidate.ID) { // rule 3
			roots = append(roots, candidate.Snapshot)
		}
	}
	return roots
}

// Collect is the operation `gc_collect`: the removal of the objects and the payload bytes that no retained
// root reaches, in one update (LIF-GC-1 to LIF-GC-6, BUN-RMV-1, BUN-RMV-2).
func Collect(c *ops.Call) envelope.Envelope {
	c.Strict([]ops.Field{ops.Opt("roots", ops.ListOf(ops.IsRef, true))}, []string{"roots"}, nil) // LIF-GC-1
	var gone []ops.Listed
	var refs []string
	payloads, candidates := []string{}, []string{}
	c.S.Update(func(tx ops.Tx) {
		gone, refs, payloads, candidates = nil, nil, []string{}, []string{} // a storage can run the update again
		lockRemoval(tx)
		listed := tx.OwnObjects()
		own := make(map[string]string, len(listed))
		for _, o := range listed {
			own[o.Ref] = o.Kind
		}
		var absent []envelope.Reason
		requested, _ := c.Request.Get("roots")
		for _, root := range ops.Strings(requested) { // LIF-GC-4, BUN-RMV-2
			if _, kept := own[root]; !kept {
				absent = append(absent, envelope.R("unknown_ref", root)) // the dispatch gives `removed` (REL-RMV-7)
			}
		}
		if len(absent) > 0 {
			ops.FailWith("not_found", absent...)
		}
		retained := references(tx, retainedRoots(c, tx, own))
		named := set{}
		if c.Store.Output != nil { // BUN-RMV-1 rules 2 and 3: the objects and the open contexts of the local output store
			var output ops.Reader
			if view, has := tx.(ops.OutputView); has {
				output, _ = view.Output()
			}
			if output == nil {
				output = c.On(c.Store.Output).S
			}
			var kept []string
			for _, o := range output.OwnObjects() {
				kept = append(kept, o.Ref)
			}
			payloadsOf(output, kept, named) // BUN-RMV-2: the bytes that an object of the local output store names
			kept = append(kept, output.OpenRoots(c.Store.Output.Millis())...)
			for ref := range references(output, kept) {
				retained[ref] = struct{}{}
			}
		}
		var remaining []string
		for _, o := range listed { // LIF-GC-5 action 1
			if _, kept := retained[o.Ref]; kept {
				remaining = append(remaining, o.Ref)
			} else {
				gone = append(gone, o)
				refs = append(refs, o.Ref)
			}
		}
		tx.DeleteObjects(refs)
		tx.DeleteDeferred(refs)
		sources := append([]string(nil), refs...)
		for _, ref := range tx.RemovedRefs() { // the kept `refs` of a removed object that no retained root reaches
			if _, kept := retained[ref]; !kept {
				sources = append(sources, ref)
			}
		}
		tx.DeleteEdges(sources)
		payloadsOf(tx, remaining, named)
		for _, digest := range tx.OwnBlobs() { // action 2
			if _, kept := named[digest]; !kept {
				payloads = append(payloads, digest)
			}
		}
		sort.Strings(payloads) // LIF-GC-6: each list is in ascending order
		tx.DeleteBlobs(payloads)
		snapshots := set{}
		for _, o := range gone {
			if o.Kind == "snapshot" {
				snapshots[o.Ref] = struct{}{}
			}
		}
		if len(snapshots) > 0 { // action 3
			for _, candidate := range tx.Candidates() {
				if _, removed := snapshots[candidate.Snapshot]; removed {
					candidates = append(candidates, candidate.ID)
				}
			}
			sort.Strings(candidates)
			tx.DeleteCandidates(candidates)
		}
		// A context whose life ended is not a read context of the store, so its record goes (CTX-LIF-5,
		// DEV-LAY-6). The call removes no read context that is open (LIF-GC-5).
		tx.DeleteContexts(tx.EndedContexts(c.Store.Millis()))
		if len(refs)+len(payloads)+len(candidates) > 0 {
			advanceEpoch(tx)
		}
	})
	c.Forget(refs)
	if c.Store.Output != nil {
		c.Store.Output.Forget(refs) // the local output store reads the objects of its device store (BUN-IMP-8)
	}
	items := make([]envelope.Item, len(gone))
	for n, o := range gone { // LIF-GC-6
		items[n] = envelope.OK(jsontext.M("ref", jsontext.S(o.Ref)), jsontext.M("kind", jsontext.S(o.Kind)))
	}
	e := c.Envelope("ok", nil, items)
	e.Fields = append(e.Fields, jsontext.M("payloads", jsontext.Strings(payloads)), jsontext.M("candidates", jsontext.Strings(candidates)))
	return e
}
