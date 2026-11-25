package read

import (
	"sort"

	"github.com/mohanraj00/aavanam/backend/core/check"
	"github.com/mohanraj00/aavanam/backend/core/envelope"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/kinds"
	"github.com/mohanraj00/aavanam/backend/core/ops"
)

// knownKinds gives one `unknown_kind` for each name of `kinds` that is not a kind of the base profile or
// of a profile that the store declares (step 4 of LIF-CHG-1, step 5 of DSC-LST-1, LYR-PRF-2, IDX-PRF-2).
func knownKinds(c *ops.Call) {
	var reasons []envelope.Reason
	for _, k := range field(c.Request, "kinds").Arr {
		if !kinds.Registered(k.Str, c.Store.Profiles) {
			reasons = append(reasons, envelope.Reason{Code: "unknown_kind"})
		}
	}
	if len(reasons) > 0 {
		ops.FailWith("unsupported", reasons...)
	}
}

// content returns the closure of a snapshot without the snapshot (LIF-ITM-1). No snapshot has no content.
func content(c *ops.Call, snapshot string) []string {
	if snapshot == "" {
		return nil
	}
	closure := c.Closure(snapshot)
	delete(closure, snapshot)
	return ops.Sorted(closure)
}

// itemKey is the key of an item (LIF-ITM-2), in the item order (LIF-ITM-3).
type itemKey struct {
	kind    string
	noID    bool   // the object has no logical ID
	id      string // the logical ID, or the reference of an object with no logical ID
	logical jsontext.Value
}

func (a itemKey) less(b itemKey) bool {
	switch {
	case a.kind != b.kind:
		return a.kind < b.kind
	case a.noID != b.noID:
		return !a.noID
	}
	return a.id < b.id
}

type keyed struct {
	key  itemKey
	refs []string // in ascending order
}

// itemKeys returns each item key of the objects and its revisions, in the item order (LIF-ITM-2, LIF-ITM-3).
func itemKeys(c *ops.Call, refs []string) []keyed {
	at := map[[3]string]int{}
	var out []keyed
	for _, ref := range refs { // ascending
		desc, _ := c.Descriptor(ref)
		lid, ok := desc.Get("logical_id")
		key := itemKey{kind: check.Kind(desc), noID: !ok, id: lid.Str, logical: lid}
		if !ok {
			key.id = ref
		}
		k := [3]string{key.kind, map[bool]string{true: "1", false: "0"}[key.noID], key.id}
		n, seen := at[k]
		if !seen {
			n = len(out)
			at[k] = n
			out = append(out, keyed{key: key})
		}
		out[n].refs = append(out[n].refs, ref)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key.less(out[j].key) })
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// SnapshotChanges is the operation `snapshot_changes` (LIF-CHG-1 to LIF-CHG-4).
func SnapshotChanges(c *ops.Call) envelope.Envelope {
	c.Strict([]ops.Field{ops.Req("before", ops.IsRef), ops.Req("after", ops.IsRef), ops.Opt("kinds", isStrings),
		ops.Opt("offset", ops.IsCount(0)), ops.Opt("limit", ops.IsCount(1))}, []string{"kinds"}, []string{"kinds"})
	knownKinds(c)
	before, after := str(c.Request, "before"), str(c.Request, "after")
	var refused []envelope.Item
	for _, ref := range []string{before, after} { // LIF-CHG-2
		switch c.Kind(ref) {
		case "":
			refused = append(refused, refusedRef("unknown_ref", ref))
		case "snapshot":
		default:
			refused = append(refused, refusedRef("unsupported_target_kind", ref))
		}
	}
	if len(refused) > 0 {
		return c.Result(refused)
	}
	old, new := itemKeys(c, content(c, before)), itemKeys(c, content(c, after))
	filter, hasFilter := c.Request.Get("kinds")
	var entries []envelope.Item
	i, j := 0, 0
	for i < len(old) || j < len(new) { // LIF-CHG-3: a merge in the item order
		var key itemKey
		var was, is []string
		switch {
		case j == len(new) || (i < len(old) && old[i].key.less(new[j].key)):
			key, was = old[i].key, old[i].refs
			i++
		case i == len(old) || new[j].key.less(old[i].key):
			key, is = new[j].key, new[j].refs
			j++
		default:
			key, was, is = old[i].key, old[i].refs, new[j].refs
			i, j = i+1, j+1
		}
		if equal(was, is) || (hasFilter && !has(filter, key.kind)) {
			continue
		}
		change := "replaced"
		if len(was) == 0 {
			change = "added"
		} else if len(is) == 0 {
			change = "removed"
		}
		lid := jsontext.Value{}
		if !key.noID {
			lid = key.logical
		}
		entries = append(entries, item(m("kind", jsontext.S(key.kind)), m("logical_id", lid), m("change", jsontext.S(change)),
			m("before", jsontext.Strings(was)), m("after", jsontext.Strings(is))))
	}
	items, bounded := Page(c, entries) // LIF-CHG-4
	return result(c, nil, items, bounded)
}

// ItemHistory is the operation `item_history` (LIF-HST-1 to LIF-HST-4).
func ItemHistory(c *ops.Call) envelope.Envelope {
	c.Strict([]ops.Field{ops.Req("collection", ops.IsStr), ops.Req("logical_id", isNonEmpty)}, nil, nil)
	lid := str(c.Request, "logical_id")
	revisions := func(snapshot string) []string { // LIF-HST-3
		var out []string
		for _, ref := range content(c, snapshot) {
			desc, _ := c.Descriptor(ref)
			if id, ok := desc.Get("logical_id"); ok && id.Str == lid {
				out = append(out, ref)
			}
		}
		return out
	}
	var items []envelope.Item
	for _, p := range c.S.Publications(str(c.Request, "collection")) { // LIF-HST-2
		was, is := revisions(p.PreviousHead), revisions(p.Head)
		change := "replaced" // LIF-HST-4
		switch {
		case len(was) == 0 && len(is) == 0:
			change = "absent"
		case len(was) == 0:
			change = "added"
		case len(is) == 0:
			change = "removed"
		case equal(was, is):
			change = "unchanged"
		}
		list := make([]jsontext.Value, len(is))
		for n, ref := range is {
			list[n] = jsontext.O(m("ref", jsontext.S(ref)), m("kind", jsontext.S(c.Kind(ref))))
		}
		items = append(items, item(m("candidate", jsontext.S(p.Candidate)), m("previous_head", nullable(p.PreviousHead)),
			m("head", jsontext.S(p.Head)), m("revisions", jsontext.A(list)), m("change", jsontext.S(change))))
	}
	return c.Result(items)
}

// ListItems is the operation `list_items` (DSC-LST-1 to DSC-LST-3, DSC-FLT-5).
func ListItems(c *ops.Call) envelope.Envelope {
	c.Strict([]ops.Field{ops.Req("context", ops.IsStr), ops.Opt("kinds", isStrings),
		ops.Opt("logical_ids", ops.ListOf(isNonEmpty, true)), ops.Opt("offset", ops.IsCount(0)),
		ops.Opt("limit", ops.IsCount(1)), ops.Opt("filter", IsFilter)},
		[]string{"kinds", "logical_ids"}, []string{"kinds", "logical_ids"})
	rc := c.Context() // context_expired before unknown_kind
	knownKinds(c)
	kindFilter, hasKinds := c.Request.Get("kinds")
	ids, hasIDs := c.Request.Get("logical_ids")
	members := rc.Members()
	var entries []envelope.Item
	var refs []string
	for _, k := range itemKeys(c, members) { // DSC-LST-2, DSC-LST-3
		if hasKinds && !has(kindFilter, k.key.kind) {
			continue
		}
		if hasIDs && (k.key.noID || !has(ids, k.key.id)) {
			continue
		}
		lid := jsontext.Value{}
		if !k.key.noID {
			lid = k.key.logical
		}
		for _, ref := range k.refs {
			entries = append(entries, item(m("ref", jsontext.S(ref)), m("kind", jsontext.S(k.key.kind)), m("logical_id", lid)))
			refs = append(refs, ref)
		}
	}
	if filter, ok := c.Request.Get("filter"); ok { // DSC-FLT-5
		kept := entries[:0:0]
		for n, o := range Filtered(c, filter, refs, members) {
			switch {
			case o.Failed != nil:
				kept = append(kept, *o.Failed)
			case o.Match:
				kept = append(kept, entries[n])
			}
		}
		entries = kept
	}
	items, bounded := Page(c, entries)
	return result(c, rc, items, bounded)
}
