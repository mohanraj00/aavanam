package read

import (
	"unicode/utf8"

	"github.com/mohanraj00/aavanam/backend/core/check"
	"github.com/mohanraj00/aavanam/backend/core/envelope"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/ops"
)

// TableOf is the table of a data resource under aavanam/table/v1 (DAT-TBL-1).
type TableOf struct {
	Columns []string
	Rows    [][]jsontext.Value
}

// Row returns one row as an object with the columns as keys (CRD-SEL-5, QRY-DAT-3).
func (t *TableOf) Row(n int) jsontext.Value {
	members := make([]jsontext.Member, len(t.Columns))
	for i, column := range t.Columns {
		members[i] = m(column, t.Rows[n][i])
	}
	return jsontext.O(members...)
}

// Cell returns the value of a column in a row.
func (t *TableOf) Cell(row int, column string) jsontext.Value {
	for i, c := range t.Columns {
		if c == column {
			return t.Rows[row][i]
		}
	}
	return jsontext.Value{}
}

// HasColumn tells if the table has the column.
func (t *TableOf) HasColumn(column string) bool {
	for _, c := range t.Columns {
		if c == column {
			return true
		}
	}
	return false
}

// ParseTable returns the table of payload bytes, or false if the bytes do not agree with DAT-TBL-1
// (DAT-TBL-3).
func ParseTable(data []byte) (*TableOf, bool) {
	if !utf8.Valid(data) {
		return nil, false
	}
	value, err := jsontext.Parse(data, jsontext.Strict)
	if err != nil || value.Kind != jsontext.Object || len(value.Obj) != 3 {
		return nil, false
	}
	columns, key, rows := field(value, "columns"), field(value, "key"), field(value, "rows")
	if !value.Has("columns") || !value.Has("key") || !value.Has("rows") {
		return nil, false
	}
	if columns.Kind != jsontext.Array || len(columns.Arr) == 0 || key.Kind != jsontext.Array || len(key.Arr) == 0 ||
		rows.Kind != jsontext.Array {
		return nil, false
	}
	position := map[string]int{}
	t := &TableOf{}
	for i, c := range columns.Arr {
		if c.Kind != jsontext.String {
			return nil, false
		}
		if _, dup := position[c.Str]; dup {
			return nil, false
		}
		position[c.Str] = i
		t.Columns = append(t.Columns, c.Str)
	}
	var positions []int
	seen := map[string]bool{}
	for _, k := range key.Arr {
		at, ok := position[k.Str]
		if k.Kind != jsontext.String || !ok || seen[k.Str] {
			return nil, false
		}
		seen[k.Str] = true
		positions = append(positions, at)
	}
	keys := map[string]struct{}{}
	for _, r := range rows.Arr {
		if r.Kind != jsontext.Array || len(r.Arr) != len(t.Columns) {
			return nil, false
		}
		cells := make([]jsontext.Value, len(positions))
		for i, at := range positions {
			cells[i] = r.Arr[at]
		}
		keys[string(canonOf(jsontext.A(cells)))] = struct{}{}
		t.Rows = append(t.Rows, r.Arr)
	}
	if len(keys) != len(t.Rows) {
		return nil, false
	}
	return t, true
}

// isReadSelector checks the form of a read selector (CRD-SEL-1, REL-SEL-1, REL-SEL-3, ADP-SEL-4).
func isReadSelector(v jsontext.Value) bool {
	if profile := field(v, "selector_profile"); v.Kind == jsontext.Object && profile.Kind == jsontext.String && profile.Str == Row {
		return len(v.Obj) == 2 && v.Has("index") && isCount(field(v, "index"), 0)
	}
	return check.IsSelector(v)
}

// Read is the operation `read` (CRD-READ-1 to CRD-READ-5).
func Read(c *ops.Call) envelope.Envelope {
	c.Strict([]ops.Field{ops.Req("context", ops.IsStr), ops.Req("ref", ops.IsRef), ops.Opt("selector", isReadSelector)}, nil, nil)
	rc := c.Context()
	ref := str(c.Request, "ref")
	selector, hasSelector := c.Request.Get("selector")
	return result(c, rc, []envelope.Item{readItem(c, rc, ref, selector, hasSelector)}, false)
}

// readItem applies the checks of CRD-READ-2 in order and gives the item of CRD-READ-3.
func readItem(c *ops.Call, rc *ops.ReadContext, ref string, selector jsontext.Value, hasSelector bool) envelope.Item {
	if code := rc.Locate(ref); code != "" {
		return envelope.Refused([]string{code}, ref)
	}
	desc, _ := c.Descriptor(ref)
	kind := check.Kind(desc)
	profile := str(selector, "selector_profile")
	target := !hasSelector
	switch {
	case !hasSelector:
	case profile == check.Bytes:
		target = desc.Has("payload")
	case profile == check.NFCText:
		target = kind == "reading"
	case profile == Row:
		target = kind == "data-resource"
	}
	if !target {
		return envelope.Refused([]string{"unsupported_selector"}, ref) // CRD-SEL-2
	}
	if hasSelector && profile == Row && str(desc, "data_profile") != Table {
		return envelope.Refused([]string{"unsupported_profile"}, ref) // CRD-SEL-5, DAT-PRF-2
	}
	payload, hasPayload := check.PayloadOf(desc, "payload")
	sel := jsontext.Value{}
	if hasSelector {
		sel = selector
	}
	fields := func(data, row jsontext.Value) envelope.Item {
		return item(m("ref", jsontext.S(ref)), m("kind", jsontext.S(kind)), m("descriptor", desc), m("selector", sel),
			m("data", data), m("row", row))
	}
	if hasSelector && profile == Row {
		if !hasPayload {
			return envelope.Refused([]string{"payload_form"}, ref) // DAT-TBL-3
		}
		table, code := loadTable(c, ref, payload) // `payload_missing`, then `payload_form` (DAT-TBL-3)
		if code != "" {
			return envelope.Refused([]string{code}, ref)
		}
		index := count(field(selector, "index")) // an index beyond int64 is beyond each table
		if index >= int64(table.Count) {
			return envelope.Refused([]string{"selector_range"}, ref)
		}
		return fields(jsontext.Value{}, table.Row(int(index))) // CRD-SEL-5
	}
	var data []byte
	if hasPayload {
		var ok bool
		if data, ok = c.Blob(payload.Digest, ref); !ok {
			return envelope.Refused([]string{"payload_missing"}, ref)
		}
	}
	if hasSelector {
		selected, failed := Select(c, ref, selector) // CRD-SEL-3, CRD-SEL-4
		if failed != nil {
			return *failed
		}
		data = selected
	}
	if data == nil && !hasPayload {
		return fields(jsontext.Value{}, jsontext.Value{}) // CRD-READ-5
	}
	return fields(jsontext.S(ops.B64(data)), jsontext.Value{}) // CRD-READ-3
}

// Select applies a selector to a target (REL-SEL-2 to REL-SEL-4, ADP-SEL-4). It returns the selected
// bytes, or the item of a failed check.
func Select(c *ops.Call, ref string, selector jsontext.Value) ([]byte, *envelope.Item) {
	fail := func(code string) ([]byte, *envelope.Item) {
		out := envelope.Refused([]string{code}, ref)
		return nil, &out
	}
	desc, _ := c.Descriptor(ref)
	profile := str(selector, "selector_profile")
	if (profile != check.Bytes && profile != check.NFCText) || (profile == check.NFCText && check.Kind(desc) != "reading") {
		return fail("unsupported_selector")
	}
	payload, ok := check.PayloadOf(desc, "payload")
	if !ok {
		return fail("unsupported_selector")
	}
	data, ok := c.Blob(payload.Digest, ref)
	if !ok {
		return fail("payload_missing")
	}
	// A bound beyond int64 is beyond each payload. The payload form comes first (CRD-SEL-4).
	start, end := count(field(selector, "start")), count(field(selector, "end"))
	if profile == check.NFCText {
		if !utf8.Valid(data) || !check.IsNFC(data) {
			return fail("payload_form")
		}
		text := []rune(string(data))
		if end > int64(len(text)) {
			return fail("selector_range")
		}
		return []byte(string(text[start:end])), nil
	}
	if end > int64(len(data)) {
		return fail("selector_range")
	}
	return append([]byte{}, data[start:end]...), nil
}
