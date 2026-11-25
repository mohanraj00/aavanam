// Package envelope has the result envelope, the items, the reasons and the status rules of document 03.
package envelope

import (
	"github.com/mohanraj00/aavanam/backend/core/canon"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/kinds"
)

// Order is the order of rule 6 of STS-ENV-3.
var Order = []string{"failed", "unsupported", "incompatible", "ambiguous", "unresolved", "not_found"}

// StatusOf returns the status of a reason code in the reason table, or in the reason table of a profile
// (STS-RSN-3, LYR-PRF-3, IDX-PRF-3). A code that is not in a table is a defect of the caller, so it panics.
func StatusOf(code string) string {
	if status, ok := reasonStatus[code]; ok {
		return status
	}
	for _, reasons := range profileReasons {
		if status, ok := reasons[code]; ok {
			return status
		}
	}
	panic("envelope: the reason table has no code " + code)
}

// Reason is one reason of an envelope or of an item (STS-RSN-1).
type Reason struct {
	Code         string
	Refs         []string
	Capabilities []string
}

// R returns a reason with references.
func R(code string, refs ...string) Reason { return Reason{Code: code, Refs: refs} }

// Value returns the JSON value of the reason.
func (r Reason) Value() jsontext.Value {
	return jsontext.O(jsontext.M("code", jsontext.S(r.Code)), jsontext.M("refs", jsontext.Strings(r.Refs)),
		jsontext.M("capabilities", jsontext.Strings(r.Capabilities)))
}

func reasonValues(reasons []Reason) jsontext.Value {
	out := make([]jsontext.Value, len(reasons))
	for i, r := range reasons {
		out[i] = r.Value()
	}
	return jsontext.A(out)
}

// Item is one item of a result (STS-ENV-2). Fields has the fields that the operation adds, in order.
type Item struct {
	Status  string
	Reasons []Reason
	Fields  []jsontext.Member
}

// OK returns an item with status `ok`.
func OK(fields ...jsontext.Member) Item { return Item{Status: "ok", Fields: fields} }

// Ref returns an `ok` item with the field `ref`.
func Ref(ref string) Item { return OK(jsontext.M("ref", jsontext.S(ref))) }

// Refused returns an item with one reason for each code. Each reason has the same references. The item has
// the status of its first code (STS-RSN-3).
func Refused(codes []string, refs ...string) Item {
	reasons := make([]Reason, len(codes))
	for i, code := range codes {
		reasons[i] = Reason{Code: code, Refs: refs}
	}
	return Item{Status: StatusOf(codes[0]), Reasons: reasons}
}

// Value returns the JSON value of the item.
func (i Item) Value() jsontext.Value {
	members := []jsontext.Member{jsontext.M("status", jsontext.S(i.Status)), jsontext.M("reasons", reasonValues(i.Reasons))}
	return jsontext.O(append(members, i.Fields...)...)
}

// Envelope is the result of one call (STS-ENV-1).
type Envelope struct {
	Status    string
	Reasons   []Reason
	Operation jsontext.Value // the operation name, or null if the call gave no name
	Context   jsontext.Value // the identity of the read context, or null
	HasItems  bool           // an envelope of STS-MAL-4 has no item list
	Items     []Item
	Fields    []jsontext.Member // the fields that the operation adds, for example `document`
}

// New returns an envelope with no item list and no context.
func New(operation jsontext.Value, status string, reasons ...Reason) Envelope {
	return Envelope{Status: status, Reasons: reasons, Operation: operation}
}

// WithItems returns the envelope with an item list. A nil list gives an empty list.
func (e Envelope) WithItems(items []Item) Envelope {
	e.HasItems, e.Items = true, items
	return e
}

// WithContext returns the envelope with the identity of a read context.
func (e Envelope) WithContext(context string) Envelope {
	e.Context = jsontext.S(context)
	return e
}

// Value returns the JSON value of the envelope.
func (e Envelope) Value() jsontext.Value {
	members := []jsontext.Member{
		jsontext.M("status", jsontext.S(e.Status)), jsontext.M("reasons", reasonValues(e.Reasons)),
		jsontext.M("operation", e.Operation), jsontext.M("operation_profile", jsontext.S(kinds.BaseProfile)),
		jsontext.M("context", e.Context),
	}
	members = append(members, e.Fields...)
	if e.HasItems {
		items := make([]jsontext.Value, len(e.Items))
		for i, item := range e.Items {
			items[i] = item.Value()
		}
		members = append(members, jsontext.M("items", jsontext.A(items)))
	}
	return jsontext.O(members...)
}

// JSON returns the envelope as JSON text. A string with a lone surrogate, for example a context identity of
// a request, has an escape, so the text is valid UTF-8.
func (e Envelope) JSON() []byte { return canon.Canonical(e.Value()) }

// FirstStatus applies rule 6 of STS-ENV-3 to outcomes that are not `ok`: the first status in Order, with
// the reasons of the outcomes that have it. With no such outcome the status is empty.
func FirstStatus(outcomes []Item) (string, []Reason) {
	for _, status := range Order {
		var reasons []Reason
		found := false
		for _, o := range outcomes {
			if o.Status == status {
				found = true
				reasons = append(reasons, o.Reasons...)
			}
		}
		if found {
			return status, reasons
		}
	}
	return "", nil
}

// Summary applies rules 4 to 6 of STS-ENV-3. `partial` tells if the operation permits the status `partial`.
func Summary(items []Item, partial bool) (string, []Reason) {
	var bad []Item
	for _, i := range items {
		if i.Status != "ok" {
			bad = append(bad, i)
		}
	}
	switch {
	case len(bad) == 0:
		return "ok", nil
	case partial && len(bad) < len(items):
		return "partial", []Reason{{Code: "items_not_ok"}}
	}
	return FirstStatus(bad)
}

// Joined returns one item from the outcomes of two targets: the first status, with one reason for each
// code (CHG-SEL-1, CHG-SEL-3).
func Joined(outcomes []Item) Item {
	status, reasons := FirstStatus(outcomes)
	var out []Reason
	at := map[string]int{}
	for _, r := range reasons {
		n, seen := at[r.Code]
		if !seen {
			n = len(out)
			at[r.Code] = n
			out = append(out, Reason{Code: r.Code})
		}
		out[n].Refs = append(out[n].Refs, r.Refs...)
	}
	return Item{Status: status, Reasons: out}
}

// Failure is an outcome of the operation as a whole (STS-ENV-3 rule 1).
type Failure struct {
	Status  string
	Reasons []Reason
}

func (f *Failure) Error() string { return f.Status + ": " + f.Reasons[0].Code }

// Fail returns a failure with one reason for each code and the status of the first code.
func Fail(codes ...string) *Failure { return FailRefs(nil, codes...) }

// FailRefs returns a failure whose reasons have the same references.
func FailRefs(refs []string, codes ...string) *Failure {
	reasons := make([]Reason, len(codes))
	for i, code := range codes {
		reasons[i] = Reason{Code: code, Refs: refs}
	}
	return &Failure{Status: StatusOf(codes[0]), Reasons: reasons}
}
