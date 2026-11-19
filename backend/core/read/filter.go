package read

import (
	"math/big"
	"regexp"
	"strconv"

	"github.com/mohanraj00/aavanam/backend/core/envelope"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/ops"
)

var (
	decimalForm = regexp.MustCompile(`\A-?(0|[1-9][0-9]*)(\.[0-9]+)?\z`)
	dayForm     = regexp.MustCompile(`\A([0-9]{4})-([0-9]{2})-([0-9]{2})\z`)
)

// typedValue is a value in the order of its declared type: a decimal by its exact numeric value
// (DAT-DEC-2), or a date in calendar order (DAT-DAY-2).
type typedValue struct {
	dec *big.Rat
	day int
}

func (a typedValue) cmp(b typedValue) int {
	if a.dec != nil {
		return a.dec.Cmp(b.dec)
	}
	switch {
	case a.day < b.day:
		return -1
	case a.day > b.day:
		return 1
	}
	return 0
}

func daysIn(year, month int) int {
	switch month {
	case 4, 6, 9, 11:
		return 30
	case 2:
		if year%4 == 0 && (year%100 != 0 || year%400 == 0) {
			return 29
		}
		return 28
	}
	return 31
}

// typed returns a value in the order of its declared type, or false if it is not a value of the type
// (DAT-DEC-1, DAT-DAY-1).
func typed(kind string, v jsontext.Value) (typedValue, bool) {
	if v.Kind != jsontext.String {
		return typedValue{}, false
	}
	if kind == "decimal" {
		if len(v.Str) > 64 || !decimalForm.MatchString(v.Str) {
			return typedValue{}, false
		}
		r, ok := new(big.Rat).SetString(v.Str)
		return typedValue{dec: r}, ok
	}
	found := dayForm.FindStringSubmatch(v.Str)
	if found == nil {
		return typedValue{}, false
	}
	year, _ := strconv.Atoi(found[1])
	month, _ := strconv.Atoi(found[2])
	day, _ := strconv.Atoi(found[3])
	if year < 1 || month < 1 || month > 12 || day < 1 || day > daysIn(year, month) {
		return typedValue{}, false
	}
	return typedValue{day: year*10000 + month*100 + day}, true
}

// valueFields returns the value fields of a range clause with a match (DAT-RNG-1).
func valueFields(match jsontext.Value) []string {
	if match.Kind != jsontext.String {
		return nil
	}
	switch match.Str {
	case "contains":
		return []string{"value"}
	case "overlaps":
		return []string{"start", "end"}
	}
	return nil
}

// IsRangeClause checks the form of a range clause (DAT-RNG-1).
func IsRangeClause(clause jsontext.Value) bool {
	kind := field(clause, "type")
	if clause.Kind != jsontext.Object || kind.Kind != jsontext.String || (kind.Str != "decimal" && kind.Str != "date") {
		return false
	}
	values := valueFields(field(clause, "match"))
	if values == nil || len(clause.Obj) != 4+len(values) {
		return false
	}
	for _, name := range append([]string{"lower", "upper", "type", "match"}, values...) {
		if !clause.Has(name) {
			return false
		}
	}
	if field(clause, "lower").Kind != jsontext.String || field(clause, "upper").Kind != jsontext.String {
		return false
	}
	for _, name := range values {
		if _, ok := typed(kind.Str, field(clause, name)); !ok {
			return false
		}
	}
	return true
}

// rangeMatches applies the half-open match of DAT-RNG-2 and DAT-RNG-3. An empty range or interval gives
// false (DAT-RNG-4).
func rangeMatches(clause jsontext.Value, lower, upper typedValue) bool {
	kind := str(clause, "type")
	if str(clause, "match") == "contains" {
		v, _ := typed(kind, field(clause, "value"))
		return lower.cmp(v) <= 0 && v.cmp(upper) < 0
	}
	start, _ := typed(kind, field(clause, "start"))
	end, _ := typed(kind, field(clause, "end"))
	if start.cmp(lower) > 0 {
		lower = start
	}
	if end.cmp(upper) < 0 {
		upper = end
	}
	return lower.cmp(upper) < 0
}

// IsFilter checks the form of a filter (DSC-FLT-1).
func IsFilter(v jsontext.Value) bool {
	if v.Kind != jsontext.Object || (!v.Has("equals") && !v.Has("ranges")) {
		return false
	}
	for _, mem := range v.Obj {
		if mem.Key != "equals" && mem.Key != "ranges" && mem.Key != "precedence" {
			return false
		}
	}
	if equals, ok := v.Get("equals"); ok {
		if equals.Kind != jsontext.Object || len(equals.Obj) == 0 || !jsontext.IsIJSON(equals) {
			return false
		}
		for _, mem := range equals.Obj {
			if mem.Val.Kind == jsontext.Null {
				return false
			}
		}
	}
	if ranges, ok := v.Get("ranges"); ok && !ops.ListOf(IsRangeClause, false)(ranges) {
		return false
	}
	if precedence, ok := v.Get("precedence"); ok && !isStrings(precedence) {
		return false
	}
	return true
}

// overlayValue applies BND-OVL-3 to the overlays that have the key. False is `overlay_conflict`.
func overlayValue(candidates []jsontext.Value, key string, precedence jsontext.Value) (jsontext.Value, bool) {
	valueOf := func(o jsontext.Value) jsontext.Value { return field(field(o, "values"), key) }
	distinct := func(list []jsontext.Value) int {
		seen := map[string]struct{}{}
		for _, o := range list {
			seen[string(canonOf(valueOf(o)))] = struct{}{}
		}
		return len(seen)
	}
	if distinct(candidates) == 1 {
		return valueOf(candidates[0]), true
	}
	tier := func(o jsontext.Value) int {
		p, ok := o.Get("precedence")
		if ok {
			for i, x := range precedence.Arr {
				if x.Kind == jsontext.String && x.Str == p.Str {
					return i
				}
			}
		}
		return len(precedence.Arr)
	}
	least := len(precedence.Arr)
	for _, o := range candidates {
		if t := tier(o); t < least {
			least = t
		}
	}
	var kept []jsontext.Value
	for _, o := range candidates {
		if tier(o) == least {
			kept = append(kept, o)
		}
	}
	if distinct(kept) == 1 {
		return valueOf(kept[0]), true
	}
	return jsontext.Value{}, false
}

// overlays returns the overlays of the read context with an object as their `values`, for each subject.
// Only the overlays of the read context give a value.
func overlays(c *ops.Call, members []string) map[string][]jsontext.Value {
	about := map[string][]jsontext.Value{}
	for _, r := range Records(c, members, "overlay") {
		if field(r.Desc, "values").Kind == jsontext.Object {
			subject := str(r.Desc, "subject")
			about[subject] = append(about[subject], r.Desc)
		}
	}
	return about
}

// metadata applies BND-OVL-3 to each key. It returns the values, or the item of a failed check:
// `no_metadata`, then `overlay_conflict`, with one reason for each affected key.
func metadata(about []jsontext.Value, keys []string, precedence jsontext.Value, ref string) ([]jsontext.Member, *envelope.Item) {
	found := make([][]jsontext.Value, len(keys))
	absent := 0
	for i, key := range keys {
		for _, o := range about {
			if field(o, "values").Has(key) {
				found[i] = append(found[i], o)
			}
		}
		if len(found[i]) == 0 {
			absent++
		}
	}
	if absent > 0 {
		out := repeated("no_metadata", absent, ref)
		return nil, &out
	}
	values := make([]jsontext.Member, len(keys))
	conflicts := 0
	for i, key := range keys {
		value, ok := overlayValue(found[i], key, precedence)
		if !ok {
			conflicts++
		}
		values[i] = m(key, value)
	}
	if conflicts > 0 {
		out := repeated("overlay_conflict", conflicts, ref)
		return nil, &out
	}
	return values, nil
}

// Outcome is the outcome of a filter for one reference: a match, no match, or the item of a failed check.
type Outcome struct {
	Ref    string
	Match  bool
	Failed *envelope.Item
}

func valueOf(values []jsontext.Member, key string) jsontext.Value {
	for _, mem := range values {
		if mem.Key == key {
			return mem.Val
		}
	}
	return jsontext.Value{}
}

// Filtered applies a filter to each reference with the overlays of the read context (DSC-FLT-2 to
// DSC-FLT-4).
func Filtered(c *ops.Call, filter jsontext.Value, refs, members []string) []Outcome {
	equals := field(filter, "equals")
	ranges := field(filter, "ranges")
	precedence := field(filter, "precedence")
	var keys []string
	seen := map[string]bool{}
	add := func(key string) {
		if !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	for _, mem := range equals.Obj {
		add(mem.Key)
	}
	for _, clause := range ranges.Arr {
		add(str(clause, "lower"))
		add(str(clause, "upper"))
	}
	about := overlays(c, members)
	out := make([]Outcome, 0, len(refs))
	for _, ref := range refs {
		values, failed := metadata(about[ref], keys, precedence, ref)
		if failed != nil {
			out = append(out, Outcome{Ref: ref, Failed: failed})
			continue
		}
		missing := 0
		for _, mem := range values {
			if mem.Val.Kind == jsontext.Null {
				missing++
			}
		}
		if missing > 0 { // DAT-MIS-2
			f := repeated("missing_value", missing, ref)
			out = append(out, Outcome{Ref: ref, Failed: &f})
			continue
		}
		wrong := 0
		for _, clause := range ranges.Arr {
			for _, name := range []string{"lower", "upper"} {
				if _, ok := typed(str(clause, "type"), valueOf(values, str(clause, name))); !ok {
					wrong++
				}
			}
		}
		if wrong > 0 { // DAT-RNG-5
			f := repeated("value_type", wrong, ref)
			out = append(out, Outcome{Ref: ref, Failed: &f})
			continue
		}
		match := true
		for _, mem := range equals.Obj {
			match = match && same(valueOf(values, mem.Key), mem.Val)
		}
		for _, clause := range ranges.Arr {
			kind := str(clause, "type")
			lower, _ := typed(kind, valueOf(values, str(clause, "lower")))
			upper, _ := typed(kind, valueOf(values, str(clause, "upper")))
			match = match && rangeMatches(clause, lower, upper)
		}
		out = append(out, Outcome{Ref: ref, Match: match})
	}
	return out
}
