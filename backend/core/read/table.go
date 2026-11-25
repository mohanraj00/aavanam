package read

import (
	"errors"

	"github.com/mohanraj00/aavanam/backend/core/check"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/ops"
)

// table is the table of a data resource for one call. It comes from the row cache of the storage, or from
// a parse of the payload bytes (DAT-TBL-3).
type table struct {
	Columns []string
	Count   int
	parsed  *TableOf // the full table, if the call parsed the payload or read each row
	cache   ops.RowCache
	digest  string
}

// HasColumn tells if the table has the column.
func (t *table) HasColumn(column string) bool {
	for _, c := range t.Columns {
		if c == column {
			return true
		}
	}
	return false
}

// rowOf returns the values of one row text of the row cache.
func rowOf(text []byte, columns int) []jsontext.Value {
	value, err := jsontext.Parse(text, jsontext.Strict)
	if err != nil || value.Kind != jsontext.Array || len(value.Arr) != columns {
		ops.Raise(errRow)
	}
	return value.Arr
}

var errRow = errors.New("a row of the row cache does not agree with its table")

// Row returns one row as an object with the columns as keys (CRD-SEL-5). The index is less than Count.
func (t *table) Row(n int) jsontext.Value {
	if t.parsed != nil {
		return t.parsed.Row(n)
	}
	text, ok := t.cache.TableRow(t.digest, n)
	if !ok {
		ops.Raise(errRow)
	}
	one := TableOf{Columns: t.Columns, Rows: [][]jsontext.Value{rowOf(text, len(t.Columns))}}
	return one.Row(0)
}

// All returns the full table, for a read of each row (QRY-DAT-3).
func (t *table) All() *TableOf {
	if t.parsed == nil {
		texts := t.cache.TableRows(t.digest)
		if len(texts) != t.Count {
			ops.Raise(errRow)
		}
		full := &TableOf{Columns: t.Columns, Rows: make([][]jsontext.Value, len(texts))}
		for i, text := range texts {
			full.Rows[i] = rowOf(text, len(t.Columns))
		}
		t.parsed = full
	}
	return t.parsed
}

// loadTable returns the table of the payload of a data resource, or the reason code of the item:
// `payload_missing` (BUN-IMP-12), then `payload_form` (DAT-TBL-3).
//
// A storage with a row cache gives the columns and the rows of a table that an earlier call parsed. The
// cache has only a payload that agrees with DAT-TBL-1, and the store has its bytes. Thus the two codes do
// not apply to a cached table, unless the descriptor has a deferred record. Then the call reads the bytes
// and makes the deferred checks, as a call with no cache does. The first parse of a table writes the cache.
func loadTable(c *ops.Call, ref string, payload check.Payload) (*table, string) {
	cache, cached := c.S.(ops.RowCache)
	if cached && !c.S.HasDeferred(ref) {
		if columns, count, ok := cache.TableHead(payload.Digest); ok {
			return &table{Columns: columns, Count: count, cache: cache, digest: payload.Digest}, ""
		}
	}
	data, ok := c.Blob(payload.Digest, ref)
	if !ok {
		return nil, "payload_missing"
	}
	parsed, ok := ParseTable(data)
	if !ok {
		return nil, "payload_form"
	}
	if cached {
		if _, _, ok := cache.TableHead(payload.Digest); !ok {
			rows := make([][]byte, len(parsed.Rows))
			for i, row := range parsed.Rows {
				rows[i] = canonOf(jsontext.A(row))
			}
			c.S.Update(func(tx ops.Tx) {
				if w, ok := tx.(ops.RowCache); ok {
					w.PutTable(payload.Digest, parsed.Columns, rows)
				}
			})
		}
	}
	return &table{Columns: parsed.Columns, Count: len(parsed.Rows), parsed: parsed}, ""
}
