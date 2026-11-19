package transfer

import (
	"sort"

	"github.com/mohanraj00/aavanam/backend/core/canon"
	"github.com/mohanraj00/aavanam/backend/core/check"
	"github.com/mohanraj00/aavanam/backend/core/envelope"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/ops"
	"github.com/mohanraj00/aavanam/backend/core/read"
	"github.com/mohanraj00/aavanam/backend/core/write"
)

const (
	bundleFormat   = "aavanam-bundle/v1"         // BUN-MAN-1
	redistribution = "aavanam/redistribution/v1" // BUN-RED-1
)

// declared has the redistribution values of BUN-RED-1.
var declared = map[string]bool{"redistributable": true, "no_redistribution": true, "unknown": true}

// isPermit checks the form of `permit`: a list of declared redistribution values (BUN-RED-4).
func isPermit(v jsontext.Value) bool {
	if v.Kind != jsontext.Array {
		return false
	}
	for _, x := range v.Arr {
		if x.Kind != jsontext.String || !declared[x.Str] {
			return false
		}
	}
	return true
}

// isValues checks the form of the values of a redistribution observation (BUN-RED-1).
func isValues(v jsontext.Value) bool {
	if v.Kind != jsontext.Object || len(v.Obj) != 2 || !v.Has("license") || !v.Has("redistribution") {
		return false
	}
	license, value := field(v, "license"), field(v, "redistribution")
	return (license.Kind == jsontext.Null || (license.Kind == jsontext.String && license.Str != "")) &&
		value.Kind == jsontext.String && declared[value.Str]
}

// redistributionOf returns the redistribution value and the license value of an object (BUN-RED-2). No
// value is inherited (BUN-RED-3).
func redistributionOf(c *ops.Call, ref string, statements []read.Record) (string, jsontext.Value) {
	desc, _ := c.Descriptor(ref)
	if len(check.Payloads(desc)) == 0 {
		return "no_payload", jsontext.Value{}
	}
	var mine []jsontext.Value
	for _, s := range statements {
		for _, subject := range field(s.Desc, "subjects").Arr {
			if subject.Str == ref {
				mine = append(mine, field(s.Desc, "values"))
				break
			}
		}
	}
	for _, v := range mine {
		if !isValues(v) {
			return "invalid", jsontext.Value{}
		}
	}
	if len(mine) == 0 {
		return "undeclared", jsontext.Value{}
	}
	for _, v := range mine[1:] {
		if !canonEqual(field(v, "license"), field(mine[0], "license")) ||
			field(v, "redistribution").Str != field(mine[0], "redistribution").Str {
			return "conflict", jsontext.Value{}
		}
	}
	return field(mine[0], "redistribution").Str, field(mine[0], "license")
}

func canonEqual(a, b jsontext.Value) bool {
	return string(canon.Canonical(a)) == string(canon.Canonical(b))
}

// Export is the operation `export` (BUN-EXP-1, BUN-EXP-2, BUN-RED-1 to BUN-RED-7, BUN-FLT-1 to BUN-FLT-4).
func Export(c *ops.Call) envelope.Envelope {
	c.Form("field_type", ops.Req("context", ops.IsStr), ops.Req("roots", ops.IsRefs), ops.Opt("permit", isPermit),
		ops.Opt("filter", read.IsFilter))
	permitValue, hasPermit := c.Request.Get("permit")
	if hasPermit && ops.HasDuplicate(permitValue.Arr) {
		ops.Fail("field_count") // KND-SCH-5
	}
	permit := map[string]bool{}
	for _, p := range permitValue.Arr {
		permit[p.Str] = true
	}
	rc := c.Context()
	stop := func(status string, reasons []envelope.Reason) envelope.Envelope {
		return c.Envelope(status, reasons, nil).WithContext(rc.ID)
	}
	requested := ops.Strings(field(c.Request, "roots"))
	var located []envelope.Reason
	for _, r := range requested {
		if code := rc.Locate(r); code != "" {
			located = append(located, envelope.R(code, r))
		}
	}
	if len(located) > 0 { // BUN-EXP-1
		return stop("not_found", located)
	}
	roots := requested
	filter, hasFilter := c.Request.Get("filter")
	if hasFilter { // BUN-FLT-1 to BUN-FLT-3: the roots of the bundle are the units that match
		var units []string
		for _, r := range ops.Sorted(c.Closure(requested...)) {
			if c.Kind(r) == "knowledge-unit" {
				units = append(units, r)
			}
		}
		outcomes := read.Filtered(c, filter, units, rc.Members())
		var failed []envelope.Item
		roots = nil
		for _, o := range outcomes {
			switch {
			case o.Failed != nil:
				failed = append(failed, *o.Failed)
			case o.Match:
				roots = append(roots, o.Ref)
			}
		}
		if len(failed) > 0 {
			return stop(envelope.FirstStatus(failed))
		}
		if len(roots) == 0 {
			return stop("unresolved", []envelope.Reason{envelope.R("no_match", requested...)})
		}
		matched := map[string]bool{}
		for _, r := range roots {
			matched[r] = true
		}
		var inside []envelope.Reason
		for _, r := range ops.Sorted(c.Closure(roots...)) {
			if c.Kind(r) == "knowledge-unit" && !matched[r] {
				inside = append(inside, envelope.R("unmatched_dependency", r))
			}
		}
		if len(inside) > 0 { // BUN-FLT-2 rule 3: the closure of a match has a unit that does not match
			return stop("incompatible", inside)
		}
	}
	objects := ops.Sorted(c.Closure(roots...))
	var statements []read.Record
	for _, r := range read.Records(c, objects, "observation") {
		if field(r.Desc, "observation_type").Str == redistribution {
			statements = append(statements, r)
		}
	}
	type value struct {
		redistribution string
		license        jsontext.Value
	}
	values := make([]value, len(objects)) // BUN-RED-2
	descs := make([]jsontext.Value, len(objects))
	withheld := map[string]bool{}
	for n, r := range objects {
		descs[n], _ = c.Descriptor(r)
		values[n].redistribution, values[n].license = redistributionOf(c, r, statements)
		// BUN-RED-4 and BUN-RED-5: the bytes of a withheld object
		if hasPermit && values[n].redistribution != "no_payload" && !permit[values[n].redistribution] {
			for _, p := range check.Payloads(descs[n]) {
				withheld[p.Digest] = true
			}
		}
	}
	// BUN-IMP-12: only descriptors in this closure can withhold a shared digest.
	payloads := map[string][]byte{}
	for n, r := range objects {
		for _, p := range check.Payloads(descs[n]) {
			if withheld[p.Digest] {
				continue
			}
			if data, ok := c.Blob(p.Digest, r); ok {
				payloads[p.Digest] = data
			} else {
				withheld[p.Digest] = true // BUN-RED-7: each failed descriptor withholds the shared bytes
				delete(payloads, p.Digest)
			}
		}
	}
	texts := make([]jsontext.Value, len(objects))
	for n := range objects {
		texts[n] = jsontext.S(string(canon.Canonical(descs[n])))
	}
	digests := make([]string, 0, len(payloads))
	for digest := range payloads {
		digests = append(digests, digest)
	}
	sort.Strings(digests)
	encoded := make([]jsontext.Member, len(digests))
	for n, digest := range digests {
		encoded[n] = jsontext.M(digest, jsontext.S(ops.B64(payloads[digest])))
	}
	bundle := []jsontext.Member{jsontext.M("format", jsontext.S(bundleFormat)), jsontext.M("roots", jsontext.Strings(roots)),
		jsontext.M("descriptors", jsontext.A(texts)), jsontext.M("payloads", jsontext.O(encoded...))} // BUN-EXP-2
	if len(withheld) > 0 {
		list := make([]string, 0, len(withheld))
		for digest := range withheld {
			list = append(list, digest)
		}
		sort.Strings(list)
		bundle = append(bundle, jsontext.M("withheld", jsontext.Strings(list)))
	}
	matched := map[string]bool{}
	if hasFilter {
		bundle = append(bundle, jsontext.M("filter", filter)) // BUN-FLT-4
		for _, r := range roots {
			matched[r] = true
		}
	}
	items := make([]envelope.Item, len(objects))
	for n, r := range objects { // BUN-RED-6
		held := false
		for _, p := range check.Payloads(descs[n]) {
			held = held || withheld[p.Digest]
		}
		fields := []jsontext.Member{jsontext.M("ref", jsontext.S(r)), jsontext.M("redistribution", jsontext.S(values[n].redistribution)),
			jsontext.M("license", values[n].license), jsontext.M("withheld", jsontext.B(held))}
		if hasFilter {
			fields = append(fields, jsontext.M("matched", jsontext.B(matched[r]))) // BUN-FLT-3
		}
		items[n] = envelope.OK(fields...)
	}
	e := c.Envelope("ok", nil, items).WithContext(rc.ID)
	e.Fields = append(e.Fields, jsontext.M("bundle", jsontext.O(bundle...)))
	return e
}

// isBundle checks the form of a bundle (BUN-IMP-1, BUN-MAN-1, BUN-FLT-4).
func isBundle(b jsontext.Value) bool {
	if b.Kind != jsontext.Object {
		return false
	}
	for _, mem := range b.Obj {
		switch mem.Key {
		case "format", "roots", "descriptors", "payloads", "withheld", "filter":
		default:
			return false
		}
	}
	for _, name := range []string{"format", "roots", "descriptors", "payloads"} {
		if !b.Has(name) {
			return false
		}
	}
	format, descriptors, payloads := field(b, "format"), field(b, "descriptors"), field(b, "payloads")
	if format.Kind != jsontext.String || format.Str != bundleFormat || !ops.IsRefs(field(b, "roots")) {
		return false
	}
	if filter, ok := b.Get("filter"); ok && !read.IsFilter(filter) {
		return false
	}
	if !ops.ListOf(ops.IsStr, true)(descriptors) || payloads.Kind != jsontext.Object {
		return false
	}
	for _, mem := range payloads.Obj {
		if !jsontext.IsDigest(mem.Key) {
			return false
		}
	}
	if withheld, ok := b.Get("withheld"); ok {
		if !ops.IsRefs(withheld) || ops.HasDuplicate(withheld.Arr) {
			return false
		}
		for _, digest := range withheld.Arr {
			if payloads.Has(digest.Str) {
				return false
			}
		}
	}
	return true
}

// revisions returns the revision of each descriptor text: of its normal form, or of its JSON value if a
// check fails (BUN-IMP-2).
func revisions(texts []jsontext.Value) map[string]bool {
	out := map[string]bool{}
	for _, text := range texts {
		value, formError := jsontext.ParseInner([]byte(text.Str), jsontext.Strict)
		if formError != nil {
			continue
		}
		if normal, refusal := check.Normal(value); refusal == nil {
			out[canon.Revision(normal)] = true
		} else {
			out[canon.Revision(value)] = true
		}
	}
	return out
}

// Import is the operation `import` (BUN-IMP-1 to BUN-IMP-12, BUN-UPD-1 to BUN-UPD-6).
func Import(c *ops.Call) envelope.Envelope {
	bundle, ok := c.Request.Get("bundle")
	if len(c.Request.Obj) != 1 || !ok || !isBundle(bundle) {
		ops.Fail("field_type") // BUN-IMP-1, BUN-MAN-1
	}
	batch := write.Batch{Data: map[string][]byte{}, Incomplete: true, Withheld: map[string]bool{}}
	for _, mem := range field(bundle, "payloads").Obj {
		batch.Data[mem.Key] = ops.Unb64(mem.Val)
	}
	for _, digest := range field(bundle, "withheld").Arr { // BUN-IMP-9: the full set for BUN-IMP-11
		batch.Withheld[digest.Str] = true
	}
	texts := field(bundle, "descriptors").Arr
	checked := batch.Check(c, texts, true)
	present := revisions(texts) // a root with a failed check is present (BUN-IMP-2)
	var absent []envelope.Item
	roots := ops.Strings(field(bundle, "roots"))
	for _, r := range roots {
		if !present[r] {
			absent = append(absent, envelope.Refused([]string{"bundle_incomplete"}, r))
		}
	}
	if c.Store.Role == ops.Device { // DEV-UPD-1, DEV-UPD-2: the record is in the update of the import
		session := c.S
		c.S = recorded{session, roots}
		defer func() { c.S = session }()
	}
	out := batch.Commit(c, checked, absent)
	if out.Status != "ok" { // BUN-IMP-4
		return c.Envelope(out.Status, out.Reasons, nil)
	}
	return c.Envelope("ok", nil, out.Items)
}

// recorded is the session of an import on a device store. Its update adds the record of the import after
// the content, in the same transaction (DEV-UPD-1, DEV-UPD-2). The record is not content of the store. An
// update that fails stops with a panic before the record, so the store adds no record and does not change
// (BUN-IMP-4, BUN-UPD-6).
type recorded struct {
	ops.Session
	roots []string
}

func (s recorded) Update(fn func(tx ops.Tx)) {
	s.Session.Update(func(tx ops.Tx) {
		fn(tx)
		if log, ok := tx.(ops.ImportLog); ok {
			log.PutImport(s.roots)
		}
	})
}
