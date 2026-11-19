package read

import (
	"bytes"
	"sort"

	"github.com/mohanraj00/aavanam/backend/core/canon"
	"github.com/mohanraj00/aavanam/backend/core/check"
	"github.com/mohanraj00/aavanam/backend/core/envelope"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/ops"
)

// isOmission checks the form of one omission of OBS-COV-1.
func isOmission(o jsontext.Value) bool {
	if o.Kind != jsontext.Object || !isNonEmpty(field(o, "reason")) {
		return false
	}
	for _, mem := range o.Obj {
		if mem.Key != "reason" && mem.Key != "selector" {
			return false
		}
	}
	selector, ok := o.Get("selector")
	return !ok || check.IsSelector(selector)
}

// CoverageValues checks the form of the values of a coverage observation (OBS-COV-1).
func CoverageValues(v jsontext.Value) bool {
	if v.Kind != jsontext.Object || len(v.Obj) != 4 {
		return false
	}
	for _, name := range []string{"unit", "covered", "total", "omissions"} {
		if !v.Has(name) {
			return false
		}
	}
	covered, ok := field(v, "covered").Int()
	if !isNonEmpty(field(v, "unit")) || !ok || covered < 0 {
		return false
	}
	if total := field(v, "total"); total.Kind != jsontext.Null && !isCount(total, covered) {
		return false
	}
	return ops.ListOf(isOmission, true)(field(v, "omissions"))
}

// coverageOf returns the coverage observations of the read context about a subject (OBS-COV-3).
func coverageOf(c *ops.Call, subject string, members []string) []Record {
	var out []Record
	for _, r := range Records(c, members, "observation") {
		if str(r.Desc, "observation_type") == CoverageProfile && has(field(r.Desc, "subjects"), subject) {
			out = append(out, r)
		}
	}
	return out
}

// Coverage is the operation `coverage` (OBS-COV-1 to OBS-COV-3).
func Coverage(c *ops.Call) envelope.Envelope {
	c.Form("field_type", ops.Req("context", ops.IsStr), ops.Req("subject", ops.IsRef), ops.Opt("observation_type", ops.IsStr))
	if kind, ok := c.Request.Get("observation_type"); ok && kind.Str != CoverageProfile {
		ops.Fail("unsupported_profile") // OBS-COV-2
	}
	rc := c.Context()
	subject := str(c.Request, "subject")
	if code := rc.Locate(subject); code != "" {
		panic(envelope.FailRefs([]string{subject}, code))
	}
	var items []envelope.Item
	for _, r := range coverageOf(c, subject, rc.Members()) {
		values := field(r.Desc, "values")
		if CoverageValues(values) {
			items = append(items, item(append([]jsontext.Member{m("ref", jsontext.S(r.Ref))}, values.Obj...)...))
		} else {
			items = append(items, envelope.Refused([]string{"values_form"}, r.Ref))
		}
	}
	return result(c, rc, items, false)
}

// Impact is the operation `impact` (CHG-IMP-1 to CHG-IMP-4).
func Impact(c *ops.Call) envelope.Envelope {
	c.Form("field_type", ops.Req("context", ops.IsStr), ops.Req("changed", ops.IsRefs), ops.Opt("dependents", ops.IsStr),
		ops.Opt("limit", ops.IsCount(1)))
	if dependents, ok := c.Request.Get("dependents"); ok && dependents.Str != "declared" {
		ops.Fail("unsupported_dependents") // CHG-IMP-1
	}
	rc := c.Context()
	changed := ops.Strings(field(c.Request, "changed"))
	if located := Located(rc, changed); len(located) > 0 {
		return result(c, rc, located, false) // CHG-IMP-2
	}
	distance := distances(c, rc, changed, false) // CHG-IMP-3
	for _, ref := range changed {
		delete(distance, ref)
	}
	dependents := byDistance(distance)
	_, limit := window(c)
	bounded := limit >= 0 && int64(len(dependents)) > limit
	if bounded {
		dependents = dependents[:limit]
	}
	items := make([]envelope.Item, len(dependents))
	for i, ref := range dependents { // CHG-IMP-4
		items[i] = item(m("ref", jsontext.S(ref)), m("kind", jsontext.S(c.Kind(ref))), m("distance", num(int64(distance[ref]))))
	}
	return result(c, rc, items, bounded)
}

// gap is one gap entry of a render document (RND-SCP-3).
type gap struct{ name, ref string }

// renderRoot returns the items of one root (RND-DOC-2, RND-SCP-1 to RND-SCP-4).
func renderRoot(c *ops.Call, rc *ops.ReadContext, root string, members []string) []envelope.Item {
	if code := rc.Locate(root); code != "" {
		out := envelope.Refused([]string{code}, root)
		out.Fields = []jsontext.Member{m("entry", jsontext.S("subject")), m("ref", jsontext.S(root))}
		return []envelope.Item{out}
	}
	subject := jsontext.S(root)
	out := []envelope.Item{item(m("entry", jsontext.S("subject")), m("ref", subject), m("kind", jsontext.S(c.Kind(root))))}
	var checks []Record
	for _, r := range Records(c, members, "check") {
		if str(r.Desc, "subject") == root {
			checks = append(checks, r)
			out = append(out, item(m("entry", jsontext.S("check")), m("subject", subject), m("ref", jsontext.S(r.Ref)),
				m("check_profile", field(r.Desc, "check_profile")), m("outcome", field(r.Desc, "outcome")),
				m("scope", field(r.Desc, "scope")), m("origin", field(r.Desc, "origin"))))
		}
	}
	var gaps []gap
	if len(checks) == 0 {
		gaps = append(gaps, gap{"no_check", ""})
	}
	observations := coverageOf(c, root, members)
	if len(observations) == 0 {
		gaps = append(gaps, gap{"no_coverage", ""})
	}
	for _, r := range observations {
		values := field(r.Desc, "values")
		if !CoverageValues(values) {
			gaps = append(gaps, gap{"values_form", r.Ref})
			continue
		}
		fields := []jsontext.Member{m("entry", jsontext.S("coverage")), m("subject", subject), m("ref", jsontext.S(r.Ref))}
		out = append(out, item(append(fields, values.Obj...)...))
		if field(values, "total").Kind == jsontext.Null {
			gaps = append(gaps, gap{"unknown_total", r.Ref})
		}
		if len(field(values, "omissions").Arr) > 0 {
			gaps = append(gaps, gap{"omissions", r.Ref})
		}
	}
	withdrawn := map[string]bool{}
	for _, r := range Records(c, members, "retraction", "supersession") {
		withdrawn[str(r.Desc, "target")] = true
	}
	for _, r := range checks {
		if withdrawn[r.Ref] {
			gaps = append(gaps, gap{"withdrawn", r.Ref})
		}
	}
	for _, g := range gaps {
		out = append(out, item(m("entry", jsontext.S("gap")), m("subject", subject), m("gap", jsontext.S(g.name)),
			m("ref", nullable(g.ref))))
	}
	return out
}

// Render is the operation `render` (RND-DOC-1 to RND-DOC-3).
func Render(c *ops.Call) envelope.Envelope {
	c.Strict([]ops.Field{ops.Req("context", ops.IsStr), ops.Req("roots", ops.ListOf(ops.IsRef, true)),
		ops.Opt("render_profile", ops.IsStr)}, []string{"roots"}, []string{"roots"})
	profile := RenderProfile
	if p, ok := c.Request.Get("render_profile"); ok {
		profile = p.Str
	}
	if profile != RenderProfile {
		ops.Fail("unsupported_profile")
	}
	rc := c.Context()
	members := rc.Members()
	roots := ops.Strings(field(c.Request, "roots"))
	var items []envelope.Item
	for _, root := range roots {
		items = append(items, renderRoot(c, rc, root, members)...)
	}
	status, reasons := envelope.Summary(items, true)
	document, id := jsontext.Value{}, jsontext.Value{}
	if status == "ok" || status == "partial" { // RND-DOC-3
		type entry struct {
			key   []byte
			value jsontext.Value
		}
		var entries []entry
		omitted := []string{}
		for _, i := range items {
			value := jsontext.O(i.Fields...)
			if i.Status == "ok" {
				entries = append(entries, entry{canon.Canonical(value), value})
			} else {
				omitted = append(omitted, str(value, "ref"))
			}
		}
		sort.Slice(entries, func(a, b int) bool { return bytes.Compare(entries[a].key, entries[b].key) < 0 })
		values := make([]jsontext.Value, len(entries))
		for n, e := range entries {
			values[n] = e.value
		}
		sorted := append([]string(nil), roots...)
		sort.Strings(sorted)
		sort.Strings(omitted)
		document = jsontext.O(m("format", jsontext.S("aavanam-render/v1")), m("render_profile", jsontext.S(profile)),
			m("context", jsontext.S(rc.ID)), m("roots", jsontext.Strings(sorted)), m("entries", jsontext.A(values)),
			m("omitted", jsontext.Strings(omitted)))
		id = jsontext.S(canon.Revision(document))
	}
	out := c.Envelope(status, reasons, items).WithContext(rc.ID)
	out.Fields = append(out.Fields, m("document", document), m("document_id", id))
	return out
}
