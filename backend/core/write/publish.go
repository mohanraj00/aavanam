package write

import (
	"sort"

	"github.com/mohanraj00/aavanam/backend/core/canon"
	"github.com/mohanraj00/aavanam/backend/core/envelope"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/ops"
)

// memberFields has the fields of a snapshot that name its members (PUB-ACT-2). `native_mappings` is last:
// check 5 of PUB-ACT-3 does not apply to it.
var memberFields = []string{"units", "schemas", "relation_sets", "overlay_sets", "observations", "native_mappings"}

type set = map[string]struct{}

// refsOf returns the references of the list fields of a descriptor.
func refsOf(desc jsontext.Value, fields ...string) []string {
	var out []string
	for _, field := range fields {
		if list, has := desc.Get(field); has {
			out = append(out, ops.Strings(list)...)
		}
	}
	return out
}

// textOf returns the string of a field, or "" for an absent field.
func textOf(desc jsontext.Value, field string) string {
	value, _ := desc.Get(field)
	return value.Str
}

// membersOf returns the members of a snapshot (PUB-ACT-2). No snapshot has no members.
func membersOf(c *ops.Call, snapshot string, fields []string) set {
	out := set{}
	if snapshot == "" {
		return out
	}
	desc, _ := c.Descriptor(snapshot)
	for _, ref := range refsOf(desc, fields...) {
		out[ref] = struct{}{}
	}
	return out
}

// covered returns the covered set of a lock (PUB-ACT-2): the closure of its items, without the lock
// descriptor.
func covered(c *ops.Call, lock string) set {
	desc, _ := c.Descriptor(lock)
	if c.Kind(lock) == "execution-lock" { // LCK-EXE-4, LCK-EXE-5
		return c.Closure(append([]string{textOf(desc, "binding_set")}, refsOf(desc, "context_inputs")...)...)
	}
	return c.Closure(refsOf(desc, "inputs")...) // LCK-BLD-3
}

// itemKey returns the kind and the `logical_id` of a member, and false for a member without a `logical_id`.
func itemKey(c *ops.Call, ref string) (string, bool) {
	desc, _ := c.Descriptor(ref)
	id, has := desc.Get("logical_id")
	if !has {
		return "", false
	}
	return c.Kind(ref) + " " + string(canon.Canonical(id)), true
}

// removedMembers returns the base members that the snapshot does not keep and that no member of it
// replaces (PUB-ACT-2).
func removedMembers(c *ops.Call, base, snapshot string) set {
	kept := membersOf(c, snapshot, memberFields)
	keys := set{}
	for ref := range kept {
		if key, has := itemKey(c, ref); has {
			keys[key] = struct{}{}
		}
	}
	out := set{}
	for ref := range membersOf(c, base, memberFields) {
		if _, stays := kept[ref]; stays {
			continue
		}
		key, has := itemKey(c, ref)
		if _, replaced := keys[key]; !has || !replaced {
			out[ref] = struct{}{}
		}
	}
	return out
}

// activationChecks returns the reasons of the first activation check that fails, or no reasons (PUB-ACT-3,
// PUB-ACT-4). The checks read only immutable objects and the gate declarations. They do not evaluate a
// date, a person or a payload (PUB-ACT-5).
func activationChecks(c *ops.Call, candidate ops.Candidate, activation string) []envelope.Reason {
	gated := c.S.Gated(candidate.Collection)
	if activation == "" { // check 1 (PUB-GAT-3). Without an activation, the store applies no other check.
		if gated {
			return []envelope.Reason{envelope.R("activation_required")}
		}
		return nil
	}
	act, _ := c.Descriptor(activation)
	var mismatch []envelope.Reason
	for _, wrong := range []bool{textOf(act, "candidate") != candidate.ID, textOf(act, "base") != candidate.ExpectedHead,
		textOf(act, "partition") != candidate.Collection} {
		if wrong { // check 2: one reason for each field that does not agree
			mismatch = append(mismatch, envelope.R("activation_mismatch", activation))
		}
	}
	if len(mismatch) > 0 {
		return mismatch
	}
	inLock := covered(c, textOf(act, "lock"))
	closure := c.Closure(candidate.Snapshot)
	var snapshots, outside []string
	for _, ref := range ops.Sorted(inLock) {
		if c.Kind(ref) == "snapshot" {
			snapshots = append(snapshots, ref)
		}
		if _, in := closure[ref]; !in {
			outside = append(outside, ref)
		}
	}
	if len(snapshots) > 0 { // check 3
		return each("lock_has_snapshot", snapshots)
	}
	if len(outside) > 0 { // check 4 (KND-DEP-3)
		return each("lock_outside_snapshot", outside)
	}
	base := membersOf(c, textOf(act, "base"), memberFields)
	var uncovered []string
	for _, ref := range ops.Sorted(membersOf(c, candidate.Snapshot, memberFields[:len(memberFields)-1])) {
		_, old := base[ref]
		if _, in := inLock[ref]; !old && !in {
			uncovered = append(uncovered, ref)
		}
	}
	if len(uncovered) > 0 { // check 5: not for native mappings
		return each("change_outside_lock", uncovered)
	}
	removed := removedMembers(c, textOf(act, "base"), candidate.Snapshot)
	refused := set{}
	listed := set{}
	for _, ref := range refsOf(act, "removed") {
		listed[ref] = struct{}{}
		if _, is := removed[ref]; !is || gated {
			refused[ref] = struct{}{}
		}
	}
	for ref := range removed {
		if _, is := listed[ref]; !is {
			refused[ref] = struct{}{}
		}
	}
	return each("removal_refused", ops.Sorted(refused)) // check 6
}

// gateChecks applies check 1 and the empty rule of check 6 again, in the atomic step (PUB-ACT-3). A gate
// can start during the validation (PUB-GAT-3). `removed` has the `removed` of the activation.
func gateChecks(tx ops.Tx, collection, activation string, removed []string) []envelope.Reason {
	if !tx.Gated(collection) {
		return nil
	}
	if activation == "" {
		return []envelope.Reason{envelope.R("activation_required")}
	}
	return each("removal_refused", removed)
}

// published returns the item of step 3 of PUB-HED-3, if the store published the candidate before
// (PUB-HED-4, PUB-HED-5).
func published(r ops.Reader, candidate ops.Candidate) (envelope.Item, bool) {
	if !r.Published(candidate.ID) {
		return envelope.Item{}, false
	}
	now := head(r, candidate.Collection)
	disposition := "superseded"
	if now == candidate.Snapshot {
		disposition = "already_published"
	}
	return envelope.OK(jsontext.M("candidate", jsontext.S(candidate.ID)), jsontext.M("disposition", jsontext.S(disposition)),
		jsontext.M("head", optional(now))), true
}

// Publish is the operation `publish` (PUB-HED-2 to PUB-HED-6). The validator of the policy decides. The
// store keeps no update open while the validator runs (PUB-RCP-3).
func Publish(c *ops.Call) envelope.Envelope {
	c.Strict([]ops.Field{ops.Req("candidate", ops.IsRef), ops.Req("receipt", ops.IsRef), ops.Opt("activation", ops.IsRef)}, nil, nil) // PUB-HED-2
	identity, receipt, activation := textOf(c.Request, "candidate"), textOf(c.Request, "receipt"), textOf(c.Request, "activation")
	one := func(status string, reasons []envelope.Reason, fields ...jsontext.Member) envelope.Envelope {
		fields = append([]jsontext.Member{jsontext.M("candidate", jsontext.S(identity))}, fields...)
		return c.Result([]envelope.Item{{Status: status, Reasons: reasons, Fields: fields}})
	}

	candidate, known := c.S.Candidate(identity)
	refs, wanted := []string{receipt}, []string{"receipt"}
	if activation != "" {
		refs, wanted = append(refs, activation), append(wanted, "activation")
	}
	var missing []envelope.Reason
	if !known {
		missing = append(missing, envelope.R("unknown_candidate"))
	}
	var wrong []string
	for i, ref := range refs {
		switch c.Kind(ref) {
		case "":
			missing = append(missing, envelope.R("unknown_ref", ref))
		case wanted[i]:
		default:
			wrong = append(wrong, ref)
		}
	}
	if len(missing) > 0 { // PUB-HED-3 step 1
		return one("not_found", missing)
	}
	if len(wrong) > 0 { // step 2
		return one("unsupported", each("unsupported_target_kind", wrong))
	}
	if before, was := published(c.S, candidate); was { // step 3: no validator call and no activation check
		return c.Result([]envelope.Item{before})
	}
	if failed := activationChecks(c, candidate, activation); len(failed) > 0 { // PUB-ACT-3, before step 4
		return one("incompatible", failed)
	}
	decision := ops.Rejected
	if validate, registered := c.Store.Policy(candidate.Policy); registered {
		decision = validate(c.Ctx, identity, receipt, activation) // PUB-RCP-2
	}
	switch decision {
	case ops.Accepted:
	case ops.NoAnswer: // PUB-RCP-4: no decision, so no receipt_rejected and no change of the store
		ops.Fail("deadline_exceeded")
	default: // step 4
		return one("incompatible", []envelope.Reason{envelope.R("receipt_rejected", receipt)})
	}

	var removed []string // an update reads the store only through its own view
	if activation != "" {
		act, _ := c.Descriptor(activation)
		removed = refsOf(act, "removed")
		sort.Strings(removed)
	}
	var out envelope.Envelope
	c.S.Update(func(tx ops.Tx) { // step 5, one atomic step
		tx.LockCollections([]string{candidate.Collection})
		if again, was := published(tx, candidate); was { // 5.1
			out = c.Result([]envelope.Item{again})
			return
		}
		// Checks 2 to 5 and the exact rule of check 6 read immutable objects only.
		if failed := gateChecks(tx, candidate.Collection, activation, removed); len(failed) > 0 {
			out = one("incompatible", failed)
			return
		}
		now := head(tx, candidate.Collection)
		if now != candidate.ExpectedHead { // 5.2
			out = one("incompatible", []envelope.Reason{envelope.R("head_moved")}, jsontext.M("head", optional(now)))
			return
		}
		tx.Publish(ops.Publication{Candidate: identity, Receipt: receipt, Activation: activation, Collection: candidate.Collection,
			PreviousHead: now, Head: candidate.Snapshot}) // 5.3, PUB-LOG-2
		out = one("ok", nil, jsontext.M("disposition", jsontext.S("published")), jsontext.M("head", jsontext.S(candidate.Snapshot)))
	})
	return out
}

// Publications is the operation `publications`: the recorded publications of one collection, in the order
// of the head updates (PUB-LOG-1 to PUB-LOG-4). It reads the catalog of the store, not a read context.
func Publications(c *ops.Call) envelope.Envelope {
	c.Strict([]ops.Field{ops.Req("collection", ops.IsStr)}, nil, nil) // PUB-LOG-1
	items := []envelope.Item{}
	for _, p := range c.S.Publications(textOf(c.Request, "collection")) {
		items = append(items, envelope.OK(jsontext.M("candidate", jsontext.S(p.Candidate)), jsontext.M("receipt", jsontext.S(p.Receipt)),
			jsontext.M("activation", optional(p.Activation)), jsontext.M("previous_head", optional(p.PreviousHead)),
			jsontext.M("head", jsontext.S(p.Head))))
	}
	return c.Result(items)
}
