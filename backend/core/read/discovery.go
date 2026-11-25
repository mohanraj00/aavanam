package read

import (
	"bytes"
	"sort"

	"github.com/mohanraj00/aavanam/backend/core/check"
	"github.com/mohanraj00/aavanam/backend/core/envelope"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/ops"
)

func isWhere(v jsontext.Value) bool { return v.Kind == jsontext.Object && jsontext.IsIJSON(v) }

func isKeys(v jsontext.Value) bool {
	return ops.ListOf(ops.IsStr, false)(v) && !ops.HasDuplicate(v.Arr)
}

// QueryData is the operation `query_data` (QRY-DAT-1 to QRY-DAT-5).
func QueryData(c *ops.Call) envelope.Envelope {
	c.Form("field_type", ops.Req("context", ops.IsStr), ops.Req("resources", ops.IsRefs), ops.Opt("where", isWhere),
		ops.Opt("ranges", ops.ListOf(IsRangeClause, false)), ops.Opt("metadata", isKeys), ops.Opt("precedence", isStrings),
		ops.Opt("schema", ops.IsRef), ops.Opt("offset", ops.IsCount(0)), ops.Opt("limit", ops.IsCount(1)))
	rc := c.Context()
	members := rc.Members()
	var about map[string][]jsontext.Value
	if c.Request.Has("metadata") {
		about = overlays(c, members)
	}
	var items []envelope.Item
	bounded := false
	for _, ref := range field(c.Request, "resources").Arr {
		out, more := queryItem(c, rc, ref.Str, about)
		items = append(items, out)
		bounded = bounded || more // QRY-DAT-4
	}
	return result(c, rc, items, bounded)
}

// queryItem applies the checks of QRY-DAT-2 in order and gives the item of QRY-DAT-3. It tells if the item
// has more rows than its limit.
func queryItem(c *ops.Call, rc *ops.ReadContext, ref string, about map[string][]jsontext.Value) (envelope.Item, bool) {
	refuse := func(code string, n int) (envelope.Item, bool) { return repeated(code, n, ref), false }
	if code := rc.Locate(ref); code != "" {
		return refuse(code, 1)
	}
	desc, _ := c.Descriptor(ref)
	switch {
	case check.Kind(desc) != "data-resource":
		return refuse("unsupported_target_kind", 1)
	case c.Request.Has("schema") && str(desc, "schema") != str(c.Request, "schema"):
		return refuse("schema_mismatch", 1)
	case str(desc, "data_profile") != Table:
		return refuse("unsupported_profile", 1)
	}
	payload, hasPayload := check.PayloadOf(desc, "payload")
	if !hasPayload {
		return refuse("payload_missing", 1)
	}
	head, code := loadTable(c, ref, payload)
	if code != "" {
		return refuse(code, 1)
	}
	where, ranges := field(c.Request, "where"), field(c.Request, "ranges")
	unknown := 0
	for _, mem := range where.Obj {
		if !head.HasColumn(mem.Key) {
			unknown++
		}
	}
	for _, clause := range ranges.Arr {
		for _, name := range []string{"lower", "upper"} {
			if !head.HasColumn(str(clause, name)) {
				unknown++
			}
		}
	}
	if unknown > 0 {
		return refuse("unknown_column", unknown)
	}
	var values []jsontext.Member
	if keys, ok := c.Request.Get("metadata"); ok {
		var failed *envelope.Item
		if values, failed = metadata(about[ref], ops.Strings(keys), field(c.Request, "precedence"), ref); failed != nil {
			return *failed, false
		}
		if values == nil {
			values = []jsontext.Member{}
		}
	}
	table := head.All() // after the checks of the columns, so a refused item reads no row
	var rows []int
	for n := range table.Rows {
		match := true
		for _, mem := range where.Obj {
			match = match && same(table.Cell(n, mem.Key), mem.Val)
		}
		if match {
			rows = append(rows, n)
		}
	}
	// QRY-DAT-5: the ranges of the rows that match `where`. A missing value stops before a value type.
	missing, wrong := 0, 0
	for _, n := range rows {
		for _, clause := range ranges.Arr {
			for _, name := range []string{"lower", "upper"} {
				cell := table.Cell(n, str(clause, name))
				if cell.Kind == jsontext.Null {
					missing++
				} else if _, ok := typed(str(clause, "type"), cell); !ok {
					wrong++
				}
			}
		}
	}
	if missing > 0 {
		return refuse("missing_value", missing) // DAT-MIS-2
	}
	if wrong > 0 {
		return refuse("value_type", wrong) // DAT-RNG-5
	}
	var kept []jsontext.Value
	for _, n := range rows {
		match := true
		for _, clause := range ranges.Arr {
			kind := str(clause, "type")
			lower, _ := typed(kind, table.Cell(n, str(clause, "lower")))
			upper, _ := typed(kind, table.Cell(n, str(clause, "upper")))
			match = match && rangeMatches(clause, lower, upper)
		}
		if match {
			kept = append(kept, table.Row(n))
		}
	}
	offset, limit := window(c)
	page, more := Page(c, kept)
	next := jsontext.Value{}
	if more {
		next = num(offset + limit)
	}
	if page == nil {
		page = []jsontext.Value{}
	}
	out := item(m("ref", jsontext.S(ref)), m("rows", jsontext.A(page)), m("next", next))
	if values != nil {
		out.Fields = append(out.Fields, m("metadata", jsontext.O(values...)))
	}
	return out, more
}

// Search is the operation `search` (QRY-LEX-1 to QRY-LEX-3).
func Search(c *ops.Call) envelope.Envelope {
	c.Form("field_type", ops.Req("context", ops.IsStr), ops.Req("query", isNonEmpty), ops.Opt("search_profile", ops.IsStr),
		ops.Opt("offset", ops.IsCount(0)), ops.Opt("limit", ops.IsCount(1)))
	rc := c.Context() // the capability comes before the profile (CTX-PRJ-4)
	if profile, ok := c.Request.Get("search_profile"); ok && profile.Str != Lexical {
		ops.Fail("unsupported_profile") // QRY-LEX-1
	}
	query := []byte(str(c.Request, "query"))
	type match struct {
		count     int
		ref, kind string
	}
	var matches []match
	for _, r := range Records(c, rc.Members(), "reading", "excerpt") { // QRY-LEX-2
		payload, _ := check.PayloadOf(r.Desc, "payload")
		if data, ok := c.Blob(payload.Digest, r.Ref); ok {
			if n := bytes.Count(data, query); n > 0 {
				matches = append(matches, match{n, r.Ref, check.Kind(r.Desc)})
			}
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].count != matches[j].count {
			return matches[i].count > matches[j].count
		}
		return matches[i].ref < matches[j].ref
	})
	page, bounded := Page(c, matches)
	items := make([]envelope.Item, len(page))
	for i, x := range page { // QRY-LEX-3
		items[i] = item(m("ref", jsontext.S(x.ref)), m("kind", jsontext.S(x.kind)), m("count", num(int64(x.count))))
	}
	return result(c, rc, items, bounded)
}

// distances returns the smallest number of reference steps from the start objects, in the read context
// (QRY-BDL-3, CHG-IMP-4). The order of the result is the order of the steps.
func distances(c *ops.Call, rc *ops.ReadContext, start []string, forward bool) map[string]int {
	out := make(map[string]int, len(start))
	todo := make([]string, 0, len(start))
	for _, ref := range start {
		if _, ok := out[ref]; !ok {
			out[ref] = 0
			todo = append(todo, ref)
		}
	}
	for len(todo) > 0 {
		ref := todo[0]
		todo = todo[1:]
		next := c.S.Sources(ref)
		if forward {
			next = c.S.Targets(ref)
		}
		for _, other := range next {
			if _, ok := out[other]; !ok && rc.Has(other) {
				out[other] = out[ref] + 1
				todo = append(todo, other)
			}
		}
	}
	return out
}

// byDistance returns the references in the order of their distance, then of the reference.
func byDistance(distance map[string]int) []string {
	out := make([]string, 0, len(distance))
	for ref := range distance {
		out = append(out, ref)
	}
	sort.Slice(out, func(i, j int) bool {
		if distance[out[i]] != distance[out[j]] {
			return distance[out[i]] < distance[out[j]]
		}
		return out[i] < out[j]
	})
	return out
}

func isBudget(v jsontext.Value) bool {
	return v.Kind == jsontext.Object && len(v.Obj) == 2 && v.Has("unit") && v.Has("max") &&
		field(v, "unit").Kind == jsontext.String && isCount(field(v, "max"), 0)
}

// ContextBundle is the operation `context_bundle` (QRY-BDL-1 to QRY-BDL-4).
func ContextBundle(c *ops.Call) envelope.Envelope {
	c.Form("field_type", ops.Req("context", ops.IsStr), ops.Req("roots", ops.IsRefs), ops.Opt("budget", isBudget))
	budget, hasBudget := c.Request.Get("budget")
	if hasBudget && str(budget, "unit") != "bytes" {
		ops.Fail("unsupported_budget") // QRY-BDL-1
	}
	most := count(field(budget, "max"))
	rc := c.Context()
	roots := ops.Strings(field(c.Request, "roots"))
	if located := Located(rc, roots); len(located) > 0 {
		return result(c, rc, located, false)
	}
	distance := distances(c, rc, roots, true)
	var items []envelope.Item
	var total int64
	for _, ref := range byDistance(distance) {
		desc, _ := c.Descriptor(ref)
		var size int64
		for _, p := range check.Payloads(desc) {
			size += p.Size
		}
		if hasBudget && total+size > most {
			return result(c, rc, items, true) // QRY-BDL-4
		}
		total += size
		items = append(items, item(m("ref", jsontext.S(ref)), m("kind", jsontext.S(check.Kind(desc))), m("size", num(size))))
	}
	return result(c, rc, items, false)
}
