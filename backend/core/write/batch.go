// Package write has the operations that write content and the catalog: `put`, `create_snapshot`, and the
// checks and the commit of a batch of descriptors, which `import` also uses.
package write

import (
	"sort"

	"github.com/mohanraj00/aavanam/backend/core/canon"
	"github.com/mohanraj00/aavanam/backend/core/check"
	"github.com/mohanraj00/aavanam/backend/core/envelope"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/ops"
)

// Checked is the outcome of the checks of one descriptor of a batch. If Refused is nil, Desc is the normal
// descriptor and Ref is its revision.
type Checked struct {
	Desc    jsontext.Value
	Ref     string
	Refused *envelope.Item
}

func refused(r *check.Refusal) *envelope.Item {
	item := envelope.Refused(r.Codes, r.Refs...)
	return &item
}

// Batch is a batch of descriptors and the payload bytes that came with them.
type Batch struct {
	Data map[string][]byte // the bytes of each digest in the batch
	// Incomplete gives `bundle_incomplete` for an absent target or payload, as an import does (BUN-IMP-2).
	Incomplete bool
	// Withheld has the digests whose bytes a bundle declares as withheld (BUN-IMP-9).
	Withheld map[string]bool
}

// Outcome is the result of the commit of a batch.
type Outcome struct {
	Status  string
	Reasons []envelope.Reason
	Items   []envelope.Item
}

// sections applies the check of PUB-SEC-3 to a snapshot of a `serving` collection.
func sections(r ops.Reader, env check.Env, desc jsontext.Value) []string {
	if check.Kind(desc) != "snapshot" {
		return nil
	}
	collection, _ := desc.Get("collection")
	if section, _ := r.Section(collection.Str); section != "serving" {
		return nil
	}
	return env.SectionViolations(desc)
}

func repeat(code string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = code
	}
	return out
}

// Check applies the steps of STS-MAL-3 to each descriptor of a batch. A value is a descriptor text (a JSON
// string value) for steps 1 to 6, or a descriptor value for steps 2 to 6 (PUB-SNP-2). A target or a payload
// can be in the batch or in the store.
func (b Batch) Check(c *ops.Call, values []jsontext.Value, texts bool) []Checked {
	out := make([]Checked, len(values))
	batch := map[string]jsontext.Value{}
	for n, value := range values {
		if texts {
			parsed, formError := jsontext.ParseInner([]byte(value.Str), jsontext.Strict)
			if formError != nil {
				out[n].Refused = refused(&check.Refusal{Codes: formError.Codes})
				continue
			}
			value = parsed
		}
		desc, refusal := check.Normal(value)
		if refusal != nil {
			out[n].Refused = refused(refusal)
			continue
		}
		out[n].Desc, out[n].Ref = desc, canon.Revision(desc)
		batch[out[n].Ref] = desc
	}
	env := b.env(batch, b.Data, c.Descriptor, c.S.Blob)
	for n := range out {
		if out[n].Refused != nil {
			continue
		}
		desc := out[n].Desc
		refusal := env.References(desc)
		if refusal == nil {
			refusal = env.Payloads(desc)
		}
		if refusal == nil {
			refusal = env.Content(desc)
		}
		if refusal == nil {
			if found := sections(c.S, env, desc); len(found) > 0 {
				refusal = &check.Refusal{Codes: repeat("section_violation", len(found)), Refs: found}
			}
		}
		if refusal != nil {
			out[n].Refused = refused(refusal)
		}
	}
	return out
}

// env returns the view of the batch and the store for the checks. `bytes` has the bytes of the batch.
func (b Batch) env(batch map[string]jsontext.Value, bytes map[string][]byte, lookup func(string) (jsontext.Value, bool),
	data func(string) ([]byte, bool)) check.Env {
	return check.Env{
		Lookup: func(ref string) (jsontext.Value, bool) {
			if desc, ok := batch[ref]; ok {
				return desc, true
			}
			return lookup(ref)
		},
		Data: func(digest string) ([]byte, bool) {
			if found, ok := bytes[digest]; ok {
				return found, true
			}
			return data(digest)
		},
		Incomplete: b.Incomplete, Withheld: b.Withheld,
	}
}

// aborted returns the items of a batch that the store did not write: each descriptor that passed its
// checks has `batch_aborted` (STS-ENV-5).
func aborted(checked []Checked) []envelope.Item {
	items := make([]envelope.Item, len(checked))
	for n, c := range checked {
		if c.Refused != nil {
			items[n] = *c.Refused
		} else {
			items[n] = envelope.Refused([]string{"batch_aborted"})
		}
	}
	return items
}

// found is the panic of an update that a check stopped. It has the reason codes and the references of
// each descriptor that failed.
type found map[string]*check.Refusal

// Commit writes the batch in one update if each descriptor passed its checks: all or none (STS-MAL-2,
// STS-ENV-5). `whole` has outcomes of the batch as a whole that are not `ok`, for example of an import.
// With a status that is not `ok`, the store did not change (STS-ENV-4).
func (b Batch) Commit(c *ops.Call, checked []Checked, whole []envelope.Item) Outcome {
	for {
		var bad []envelope.Item
		for _, x := range checked {
			if x.Refused != nil {
				bad = append(bad, *x.Refused)
			}
		}
		bad = append(bad, whole...)
		if len(bad) > 0 {
			status, reasons := envelope.FirstStatus(bad)
			return Outcome{Status: status, Reasons: reasons, Items: aborted(checked)}
		}
		failed, fault := b.write(c, checked)
		if fault != nil { // STS-ENV-3 rule 1, STS-ENV-5: the store did not change
			return Outcome{Status: fault.Status, Reasons: fault.Reasons, Items: aborted(checked)}
		}
		if failed == nil {
			items := make([]envelope.Item, len(checked))
			for n, x := range checked {
				items[n] = envelope.Ref(x.Ref)
			}
			return Outcome{Status: "ok", Items: items}
		}
		// A check in the update failed, so the store did not change. Give the refusal to each descriptor.
		for n := range checked {
			if refusal, ok := failed[checked[n].Ref]; ok {
				checked[n].Refused = refused(refusal)
			}
		}
	}
}

// write runs the update of a batch. It returns the descriptors that a check in the update refused, or the
// failure of a storage fault.
func (b Batch) write(c *ops.Call, checked []Checked) (failed found, fault *envelope.Failure) {
	defer func() {
		switch p := recover().(type) {
		case nil:
		case found:
			failed = p
		case *ops.Fault:
			fault = ops.FaultFailure(p)
		default:
			panic(p)
		}
	}()
	batch := map[string]jsontext.Value{}
	var refs, digests, collections, order []string
	blobs := map[string][]byte{} // the bytes that this update can add: the payloads of the batch
	for _, x := range checked {
		if _, ok := batch[x.Ref]; ok {
			continue
		}
		batch[x.Ref] = x.Desc
		refs = append(refs, x.Ref)
		for _, p := range check.Payloads(x.Desc) {
			if data, ok := b.Data[p.Digest]; ok {
				if _, seen := blobs[p.Digest]; !seen {
					order = append(order, p.Digest)
				}
				blobs[p.Digest] = data
			}
			digests = append(digests, p.Digest)
		}
		if check.Kind(x.Desc) == "snapshot" {
			collection, _ := x.Desc.Get("collection")
			collections = append(collections, collection.Str)
		}
	}
	sort.Strings(digests)
	sort.Strings(collections)
	c.S.Update(func(tx ops.Tx) {
		// The locks come first, in ascending order. After them, the bytes, the deferred records and the
		// sections that this update reads are stable (BUN-IMP-10, PUB-SEC-2).
		tx.LockCollections(collections)
		tx.LockDigests(digests)
		lookup := func(ref string) (jsontext.Value, bool) { return ops.DescriptorIn(tx, ref) }
		env := b.env(batch, blobs, lookup, tx.Blob)
		stopped := found{}
		type pair struct{ digest, ref string }
		var deferred []pair
		if len(b.Withheld) > 0 { // BUN-IMP-11: the bytes can be in the store now
			withheld := env
			withheld.Incomplete = true
			for _, ref := range refs {
				desc := batch[ref]
				refusal := withheld.Payloads(desc)
				if refusal == nil {
					refusal = withheld.Content(desc)
				}
				if refusal != nil {
					stopped[ref] = refusal
				}
				needed := map[string]bool{}
				for _, p := range check.Payloads(desc) {
					if _, ok := env.Data(p.Digest); !ok {
						deferred = append(deferred, pair{p.Digest, ref})
					}
					needed[p.Digest] = true
				}
				if check.Kind(desc) == "excerpt" {
					parent, _ := desc.Get("parent")
					reading, _ := env.Lookup(parent.Str)
					whole, _ := check.PayloadOf(reading, "payload")
					needed[whole.Digest] = true
					var absent []string
					held := false
					for digest := range needed {
						if _, ok := env.Data(digest); !ok {
							absent = append(absent, digest)
							held = held || b.Withheld[digest]
						}
					}
					if held {
						for _, digest := range absent {
							deferred = append(deferred, pair{digest, ref})
						}
					}
				}
			}
			if len(stopped) > 0 {
				panic(stopped)
			}
		}
		// The deferred checks of stored descriptors, before the new bytes go into the store (BUN-IMP-10).
		for _, digest := range order {
			if _, ok := tx.Blob(digest); ok {
				continue // the store has the bytes, so no check waits for them
			}
			for _, ref := range tx.DeferredRefs(digest) {
				desc, _ := env.Lookup(ref)
				needed := map[string]bool{}
				for _, p := range check.Payloads(desc) {
					needed[p.Digest] = true
				}
				if check.Kind(desc) == "excerpt" {
					parent, _ := desc.Get("parent")
					reading, _ := env.Lookup(parent.Str)
					whole, _ := check.PayloadOf(reading, "payload")
					needed[whole.Digest] = true
				}
				again := env
				again.Incomplete, again.Withheld = false, map[string]bool{}
				for d := range needed {
					if _, ok := env.Data(d); !ok {
						again.Withheld[d] = true
					}
				}
				refusal := again.Payloads(desc)
				if refusal == nil {
					refusal = again.Content(desc)
				}
				if refusal == nil {
					continue
				}
				// The descriptors of the batch that carry the bytes get the reasons.
				for _, carrier := range refs {
					for _, p := range check.Payloads(batch[carrier]) {
						if p.Digest == digest {
							if stopped[carrier] == nil {
								stopped[carrier] = &check.Refusal{}
							}
							stopped[carrier].Codes = append(stopped[carrier].Codes, refusal.Codes...)
							break
						}
					}
				}
			}
		}
		if len(stopped) > 0 {
			panic(stopped)
		}
		// A second open of the store can declare a section after the checks of Check (PUB-SEC-2).
		for _, ref := range refs {
			if violations := sections(tx, env, batch[ref]); len(violations) > 0 {
				stopped[ref] = &check.Refusal{Codes: repeat("section_violation", len(violations)), Refs: violations}
			}
		}
		if len(stopped) > 0 {
			panic(stopped)
		}
		// The writes are in ascending order of the key, so two updates that write the same rows take
		// the row locks of a storage in one order and cannot wait for each other.
		ascending := append([]string(nil), refs...)
		sort.Strings(ascending)
		for _, ref := range ascending {
			desc := batch[ref]
			object := ops.Object{Ref: ref, Kind: check.Kind(desc), Text: canon.Canonical(desc), Targets: check.Targets(desc)}
			if object.Kind == "snapshot" {
				collection, _ := desc.Get("collection")
				object.Collection = collection.Str
			}
			tx.PutObject(object)
		}
		ascending = append([]string(nil), order...)
		sort.Strings(ascending)
		for _, digest := range ascending {
			waiting := tx.DeferredRefs(digest)
			tx.PutBlob(digest, blobs[digest])
			tx.ClearDeferred(digest)
			for _, ref := range waiting { // the stored descriptors whose bytes are here now
				if desc, ok := env.Lookup(ref); ok {
					index(c, tx, desc, env)
				}
			}
		}
		for _, d := range deferred {
			tx.PutDeferred(d.digest, d.ref)
		}
		for _, ref := range refs {
			index(c, tx, batch[ref], env)
		}
	})
	return nil, nil
}

// index gives each payload of a descriptor whose bytes are in the store to the indexers.
func index(c *ops.Call, tx ops.Tx, desc jsontext.Value, env check.Env) {
	indexers := c.Store.Indexers()
	if len(indexers) == 0 {
		return
	}
	for _, p := range check.Payloads(desc) {
		if data, ok := env.Data(p.Digest); ok {
			for _, f := range indexers {
				f(tx, desc, p.Digest, data)
			}
		}
	}
}
