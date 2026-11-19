package read

import (
	"crypto/rand"
	"encoding/hex"
	"sort"

	"github.com/mohanraj00/aavanam/backend/core/check"
	"github.com/mohanraj00/aavanam/backend/core/envelope"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/ops"
)

func repeat(code string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = code
	}
	return out
}

// openForm checks the request form of `open_context`: the checks of CTX-OPN-5 in order, with one reason
// for each affected value.
func openForm(request jsontext.Value) {
	fields := map[string]ops.Test{"roots": ops.IsRef, "heads": ops.IsStr, "capabilities": ops.IsStr}
	unknown := 0
	for _, m := range request.Obj {
		if _, ok := fields[m.Key]; !ok && m.Key != "skew_policy" {
			unknown++
		}
	}
	if unknown > 0 {
		ops.Fail(repeat("unknown_field", unknown)...)
	}
	types := 0
	for _, name := range []string{"roots", "heads", "capabilities"} {
		value, has := request.Get(name)
		if !has {
			continue
		}
		if value.Kind != jsontext.Array {
			types++
			continue
		}
		for _, x := range value.Arr {
			if !fields[name](x) {
				types++
			}
		}
	}
	if policy, has := request.Get("skew_policy"); has && (policy.Kind != jsontext.String || (policy.Str != "reject" && policy.Str != "permit")) {
		types++ // CTX-SKW-3
	}
	if types > 0 {
		ops.Fail(repeat("field_type", types)...)
	}
	roots, _ := request.Get("roots")
	heads, _ := request.Get("heads")
	capabilities, _ := request.Get("capabilities")
	counts := 0
	if len(roots.Arr) == 0 && len(heads.Arr) == 0 {
		counts++
	}
	for _, list := range [][]jsontext.Value{heads.Arr, capabilities.Arr} {
		seen := map[string]struct{}{}
		for _, x := range list {
			seen[x.Str] = struct{}{}
		}
		counts += len(list) - len(seen)
	}
	if counts > 0 {
		ops.Fail(repeat("field_count", counts)...)
	}
}

// OpenContext is the operation `open_context` (document 08). It resolves each head in one read, checks
// each root, and records the context with the closure of its roots as its content (CTX-OPN-1 to CTX-OPN-3).
func OpenContext(c *ops.Call) envelope.Envelope {
	openForm(c.Request)
	var capabilities []string
	if value, has := c.Request.Get("capabilities"); has {
		capabilities = ops.Strings(value)
	} else {
		for capability := range ops.Capabilities { // CTX-PRJ-2
			capabilities = append(capabilities, capability)
		}
	}
	sort.Strings(capabilities)
	var unknown []envelope.Reason
	if value, has := c.Request.Get("capabilities"); has {
		for _, capability := range ops.Strings(value) {
			if _, ok := ops.Capabilities[capability]; !ok { // CTX-PRJ-3
				unknown = append(unknown, envelope.Reason{Code: "unsupported_profile", Capabilities: []string{capability}})
			}
		}
	}
	if len(unknown) > 0 {
		ops.FailWith("unsupported", unknown...)
	}
	roots, _ := c.Request.Get("roots")
	heads, _ := c.Request.Get("heads")
	found := c.S.Heads(ops.Strings(heads)) // CTX-VIW-3
	var items []envelope.Item
	for _, root := range roots.Arr { // CTX-OPN-1
		switch c.Kind(root.Str) {
		case "":
			items = append(items, envelope.Refused([]string{"unknown_ref"}, root.Str))
		case "knowledge-unit", "snapshot":
			items = append(items, envelope.Ref(root.Str))
		default:
			items = append(items, envelope.Refused([]string{"unsupported_target_kind"}, root.Str))
		}
	}
	for _, name := range heads.Arr { // CTX-VIW-2
		if head, ok := found[name.Str]; ok {
			items = append(items, envelope.Ref(head))
		} else {
			items = append(items, envelope.Refused([]string{"no_head"}))
		}
	}
	resolved := make([]string, 0, len(items))
	for _, i := range items {
		if i.Status == "ok" {
			resolved = append(resolved, i.Fields[0].Val.Str)
		}
	}
	composed(c, items)
	allOK := true // CTX-SKW-4: after CTX-OPN-1, CTX-VIW-2 and CTX-CMP-1
	for _, i := range items {
		allOK = allOK && i.Status == "ok"
	}
	policy, hasPolicy := c.Request.Get("skew_policy")
	if allOK && (!hasPolicy || policy.Str == "reject") {
		skewed(c, items)
	}
	status, reasons := envelope.Summary(items, false)
	if status != "ok" {
		return c.Envelope(status, reasons, items)
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		ops.Raise(err)
	}
	id := "ctx-" + hex.EncodeToString(random)
	members := ops.Sorted(c.Closure(resolved...))
	c.S.Update(func(tx ops.Tx) {
		tx.PutContext(id, ops.ContextRecord{Roots: resolved, Capabilities: capabilities}, members)
	})
	return c.Envelope("ok", nil, items).WithContext(id) // CTX-OPN-2
}

type root struct {
	position int
	ref      string
}

// snapshots returns the position and the reference of each `ok` item with a snapshot root.
func snapshots(c *ops.Call, items []envelope.Item) []root {
	var out []root
	for n, i := range items {
		if i.Status == "ok" && c.Kind(i.Fields[0].Val.Str) == "snapshot" {
			out = append(out, root{n, i.Fields[0].Val.Str})
		}
	}
	return out
}

func collectionOf(c *ops.Call, ref string) string {
	desc, _ := c.Descriptor(ref)
	collection, _ := desc.Get("collection")
	return collection.Str
}

// composed gives `collection_conflict` to each of two different snapshot roots of one collection (CTX-CMP-1).
func composed(c *ops.Call, items []envelope.Item) {
	roots := snapshots(c, items)
	collections := map[string]map[string]struct{}{}
	for _, r := range roots {
		name := collectionOf(c, r.ref)
		if collections[name] == nil {
			collections[name] = map[string]struct{}{}
		}
		collections[name][r.ref] = struct{}{}
	}
	for _, r := range roots {
		if len(collections[collectionOf(c, r.ref)]) > 1 {
			items[r.position] = envelope.Refused([]string{"collection_conflict"}, r.ref)
		}
	}
}

// pins returns the pins of a snapshot root for each logical ID: the bound objects in its closure that
// have the logical ID, in ascending order (CTX-SKW-1).
func pins(c *ops.Call, snapshot string) map[string][]string {
	closure := c.Closure(snapshot)
	bound := map[string]struct{}{}
	for ref := range closure {
		desc, _ := c.Descriptor(ref)
		if check.Kind(desc) != "binding-set" {
			continue
		}
		bindings, _ := desc.Get("bindings")
		for _, m := range bindings.Obj {
			bound[m.Val.Str] = struct{}{}
		}
		profiles, _ := desc.Get("runtime_profiles")
		for _, x := range profiles.Arr {
			bound[x.Str] = struct{}{}
		}
	}
	out := map[string][]string{}
	for _, ref := range ops.Sorted(bound) {
		if _, ok := closure[ref]; !ok {
			continue
		}
		desc, _ := c.Descriptor(ref)
		if id, has := desc.Get("logical_id"); has {
			out[id.Str] = append(out[id.Str], ref)
		}
	}
	return out
}

// skewed makes each snapshot root that has pins for a logical ID with skew `incompatible`, with one reason
// `binding_skew` for each such logical ID (CTX-SKW-2, CTX-SKW-4).
func skewed(c *ops.Call, items []envelope.Item) {
	roots := snapshots(c, items)
	all := map[string]map[string][]string{}
	for _, r := range roots {
		if _, ok := all[r.ref]; !ok {
			all[r.ref] = pins(c, r.ref)
		}
	}
	sets := map[string]map[string]struct{}{} // logical ID, then the different pin sets
	for _, p := range all {
		for id, refs := range p {
			if sets[id] == nil {
				sets[id] = map[string]struct{}{}
			}
			key := ""
			for _, ref := range refs {
				key += ref + " "
			}
			sets[id][key] = struct{}{}
		}
	}
	for _, r := range roots {
		var ids []string
		for id := range all[r.ref] {
			if len(sets[id]) > 1 {
				ids = append(ids, id)
			}
		}
		sort.Strings(ids)
		var reasons []envelope.Reason
		for _, id := range ids {
			reasons = append(reasons, envelope.Reason{Code: "binding_skew", Refs: all[r.ref][id]})
		}
		if len(reasons) > 0 {
			items[r.position] = envelope.Item{Status: "incompatible", Reasons: reasons}
		}
	}
}
