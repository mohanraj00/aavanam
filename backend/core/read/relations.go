package read

import (
	"bytes"
	"sort"

	"github.com/mohanraj00/aavanam/backend/core/check"
	"github.com/mohanraj00/aavanam/backend/core/envelope"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/ops"
)

// subjectOf returns the read context and the subject of an `evidence` or `observations` request
// (REL-EVD-1, OBS-SUB-1).
func subjectOf(c *ops.Call) (*ops.ReadContext, string) {
	c.Form("field_type", ops.Req("context", ops.IsStr), ops.Req("subject", ops.IsRef))
	rc := c.Context()
	subject := str(c.Request, "subject")
	if code := rc.Locate(subject); code != "" {
		panic(envelope.FailRefs([]string{subject}, code))
	}
	return rc, subject
}

// Evidence is the operation `evidence` (REL-EVD-1 to REL-EVD-3, REL-RET-3).
func Evidence(c *ops.Call) envelope.Envelope {
	rc, subject := subjectOf(c)
	members := rc.Members() // in ascending order
	reachable := map[string]struct{}{}
	for _, r := range Records(c, members, "relation-set") {
		for _, x := range field(r.Desc, "members").Arr {
			// CTX-OPN-4, REL-RMV-13: a record that is in the store but not in the context gives no item
			if n := sort.SearchStrings(members, x.Str); n < len(members) && members[n] == x.Str {
				reachable[x.Str] = struct{}{}
			}
		}
	}
	refs := ops.Sorted(reachable)
	var items []envelope.Item
	supports := map[string]bool{}
	for _, r := range Records(c, refs, "evidence") {
		if str(r.Desc, "subject") == subject {
			supports[r.Ref] = true
			items = append(items, item(m("relation", jsontext.S("support")), m("ref", jsontext.S(r.Ref))))
		}
	}
	for _, r := range Records(c, refs, "retraction", "supersession") {
		if supports[str(r.Desc, "target")] {
			items = append(items, item(m("relation", jsontext.S("withdrawal")), m("ref", jsontext.S(r.Ref)),
				m("target", field(r.Desc, "target")), m("origin", field(r.Desc, "origin"))))
		}
	}
	return result(c, rc, items, false)
}

// Observations is the operation `observations` (OBS-SUB-1, OBS-SUB-2).
func Observations(c *ops.Call) envelope.Envelope {
	rc, subject := subjectOf(c)
	var items []envelope.Item
	for _, r := range Records(c, rc.Members(), "observation") {
		if has(field(r.Desc, "subjects"), subject) {
			items = append(items, envelope.Ref(r.Ref))
		}
	}
	return result(c, rc, items, false)
}

// Compare is the operation `compare` (CHG-SEL-1 to CHG-SEL-3).
func Compare(c *ops.Call) envelope.Envelope {
	c.Form("field_type", ops.Req("context", ops.IsStr), ops.Req("before", ops.IsRef), ops.Req("after", ops.IsRef),
		ops.Req("selector", check.IsSelector))
	rc := c.Context()
	refs := []string{str(c.Request, "before"), str(c.Request, "after")}
	selector := field(c.Request, "selector")
	var located []envelope.Item
	for _, ref := range refs {
		if code := rc.Locate(ref); code != "" {
			located = append(located, envelope.Refused([]string{code}, ref))
		}
	}
	if len(located) > 0 { // CHG-SEL-1
		return result(c, rc, []envelope.Item{envelope.Joined(located)}, false)
	}
	var selected [2][]byte
	var bad []envelope.Item
	for n, ref := range refs {
		data, failed := Select(c, ref, selector)
		if failed != nil {
			bad = append(bad, *failed)
		}
		selected[n] = data
	}
	if len(bad) > 0 { // CHG-SEL-3
		return result(c, rc, []envelope.Item{envelope.Joined(bad)}, false)
	}
	change := "changed"
	if bytes.Equal(selected[0], selected[1]) {
		change = "unchanged"
	}
	return result(c, rc, []envelope.Item{item(m("change", jsontext.S(change)))}, false)
}
