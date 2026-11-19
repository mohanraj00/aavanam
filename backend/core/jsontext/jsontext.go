// Package jsontext reads JSON text with the checks of step 1 of STS-MAL-3: `malformed_json`, `duplicate_key`
// and `non_ijson_value`.
//
// The standard decoder cannot do these checks. It changes a lone surrogate escape and invalid UTF-8 into
// U+FFFD, and it accepts a duplicate key. The reader of this package has an explicit stack and no recursion,
// so the depth of a text cannot use up the stack of the process.
package jsontext

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Kind is the type of a JSON value.
type Kind uint8

// The kinds of a JSON value.
const (
	Null Kind = iota
	Bool
	Number
	String
	Array
	Object
)

// Value is one JSON value. A number has the value of its double. The reader of a descriptor accepts a
// number only if this value is exact (see Kept).
type Value struct {
	Kind Kind
	Bool bool
	Num  float64
	// Lossy marks a number of a request that is outside I-JSON: Num is not its exact value (see Lenient).
	Lossy bool
	Str   string
	Arr   []Value
	Obj   []Member // in the order of the text, with no duplicate key
}

// Member is one key and its value in an object.
type Member struct {
	Key string
	Val Value
}

// Get returns the value of a key of an object.
func (v Value) Get(key string) (Value, bool) {
	if v.Kind != Object {
		return Value{}, false
	}
	for i := range v.Obj {
		if v.Obj[i].Key == key {
			return v.Obj[i].Val, true
		}
	}
	return Value{}, false
}

// Has tells if an object has a key.
func (v Value) Has(key string) bool {
	_, ok := v.Get(key)
	return ok
}

// IsInt tells if the value is a number with an integral value. JSON has one number type, so `1`, `1.0` and
// `1e0` are the same integer (IDN-CAN-1).
func (v Value) IsInt() bool {
	return v.Kind == Number && v.Num == math.Trunc(v.Num) && !math.IsInf(v.Num, 0)
}

// S returns a string value.
func S(s string) Value { return Value{Kind: String, Str: s} }

// N returns a number value.
func N(x float64) Value { return Value{Kind: Number, Num: x} }

// B returns a boolean value.
func B(b bool) Value { return Value{Kind: Bool, Bool: b} }

// A returns an array value. A nil list gives an empty array.
func A(items []Value) Value { return Value{Kind: Array, Arr: items} }

// O returns an object value from members in order. The caller gives no duplicate key.
func O(members ...Member) Value { return Value{Kind: Object, Obj: members} }

// M returns one member.
func M(key string, val Value) Member { return Member{Key: key, Val: val} }

// Strings returns an array of strings.
func Strings(items []string) Value {
	out := make([]Value, len(items))
	for i, s := range items {
		out[i] = S(s)
	}
	return A(out)
}

// FormError is a JSON text that fails a check of step 1 of STS-MAL-3. Codes has one code for each affected
// value, and each code is the same.
type FormError struct {
	Codes []string
}

func (e *FormError) Error() string { return e.Codes[0] }

func formError(code string, n int) *FormError {
	codes := make([]string, n)
	for i := range codes {
		codes[i] = code
	}
	return &FormError{Codes: codes}
}

// Digest returns the digest of bytes: `sha256:` and 64 lowercase hexadecimal digits (IDN-DIG-1).
func Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// IsDigest tells if a string has the form of a digest or of a reference (IDN-DIG-1).
func IsDigest(s string) bool {
	if len(s) != 71 || s[:7] != "sha256:" {
		return false
	}
	for i := 7; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// ValidString tells if a string agrees with I-JSON (RFC 7493, section 2.1): it is valid UTF-8, so it has
// no surrogate, and it has no Unicode noncharacter.
func ValidString(s string) bool {
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size <= 1 {
			return false
		}
		if (r >= 0xFDD0 && r <= 0xFDEF) || r&0xFFFE == 0xFFFE {
			return false
		}
		i += size
	}
	return true
}

// Mode selects the rules of the reader for strings and numbers.
type Mode uint8

const (
	// Strict is the reader of a descriptor: each check of step 1 of STS-MAL-3 applies.
	Strict Mode = iota
	// Lenient is the reader of a request. A request is not a descriptor, so a string with a lone surrogate
	// and a number outside I-JSON stay in the value, and the operation checks its own fields. The checks
	// `malformed_json` and `duplicate_key` still apply.
	Lenient
)

// Parse returns the value of a JSON text in UTF-8. Invalid UTF-8 gives `malformed_json`.
func Parse(text []byte, mode Mode) (Value, *FormError) {
	return parse(text, mode, false)
}

// ParseInner returns the value of a JSON text that was a JSON string of an outer text, as Unquote returns it.
// In such a text, the bytes ED A0 80 to ED BF BF are a lone surrogate that an escape of the outer text gave.
// It is a string value outside I-JSON (`non_ijson_value`), not malformed JSON.
func ParseInner(text []byte, mode Mode) (Value, *FormError) {
	return parse(text, mode, true)
}

type frame struct {
	object bool
	arr    []Value
	obj    []Member
	keys   map[string]struct{}
	key    string
}

type reader struct {
	text    []byte
	pos     int
	mode    Mode
	inner   bool
	dups    int // the keys that an object has more than one time
	outside int // the numbers and the strings outside I-JSON
}

func (r *reader) space() {
	for r.pos < len(r.text) {
		switch r.text[r.pos] {
		case ' ', '\t', '\n', '\r':
			r.pos++
		default:
			return
		}
	}
}

func (r *reader) peek() byte {
	if r.pos < len(r.text) {
		return r.text[r.pos]
	}
	return 0
}

func parse(text []byte, mode Mode, inner bool) (Value, *FormError) {
	r := &reader{text: text, mode: mode, inner: inner}
	v, ok := r.document()
	switch {
	case !ok:
		return Value{}, formError("malformed_json", 1)
	case r.dups > 0:
		return Value{}, formError("duplicate_key", r.dups)
	case r.outside > 0:
		return Value{}, formError("non_ijson_value", r.outside)
	}
	return v, nil
}

// key reads `"key" :` of an object member into the frame.
func (r *reader) key(f *frame) bool {
	r.space()
	if r.peek() != '"' {
		return false
	}
	k, ok := r.str()
	if !ok {
		return false
	}
	r.space()
	if r.peek() != ':' {
		return false
	}
	r.pos++
	f.key = k
	return true
}

func (r *reader) document() (Value, bool) {
	var stack []frame
	for {
		// Read the start of one value.
		r.space()
		var v Value
		switch c := r.peek(); {
		case r.pos >= len(r.text):
			return Value{}, false
		case c == '{':
			r.pos++
			r.space()
			if r.peek() == '}' {
				r.pos++
				v = Value{Kind: Object}
				break
			}
			stack = append(stack, frame{object: true})
			if !r.key(&stack[len(stack)-1]) {
				return Value{}, false
			}
			continue
		case c == '[':
			r.pos++
			r.space()
			if r.peek() == ']' {
				r.pos++
				v = Value{Kind: Array}
				break
			}
			stack = append(stack, frame{})
			continue
		case c == '"':
			s, ok := r.str()
			if !ok {
				return Value{}, false
			}
			v = Value{Kind: String, Str: s}
		case c == '-' || (c >= '0' && c <= '9'):
			x, ok, lossy := r.number()
			if !ok {
				return Value{}, false
			}
			v = Value{Kind: Number, Num: x, Lossy: lossy}
		case r.literal("true"):
			v = Value{Kind: Bool, Bool: true}
		case r.literal("false"):
			v = Value{Kind: Bool}
		case r.literal("null"):
			v = Value{}
		default:
			return Value{}, false
		}
		// Put the value into its container. Close each container that ends here.
		for {
			if len(stack) == 0 {
				r.space()
				return v, r.pos == len(r.text)
			}
			f := &stack[len(stack)-1]
			r.space()
			c := r.peek()
			if f.object {
				if f.keys == nil && len(f.obj) >= 8 {
					f.keys = make(map[string]struct{}, 16)
					for _, m := range f.obj {
						f.keys[m.Key] = struct{}{}
					}
				}
				seen := false
				if f.keys != nil {
					_, seen = f.keys[f.key]
				} else {
					for i := range f.obj {
						if f.obj[i].Key == f.key {
							seen = true
							break
						}
					}
				}
				if seen {
					r.dups++
				} else {
					f.obj = append(f.obj, Member{Key: f.key, Val: v})
					if f.keys != nil {
						f.keys[f.key] = struct{}{}
					}
				}
				if c == ',' {
					r.pos++
					if !r.key(f) {
						return Value{}, false
					}
					break
				}
				if c != '}' {
					return Value{}, false
				}
				r.pos++
				v = Value{Kind: Object, Obj: f.obj}
			} else {
				f.arr = append(f.arr, v)
				if c == ',' {
					r.pos++
					break
				}
				if c != ']' {
					return Value{}, false
				}
				r.pos++
				v = Value{Kind: Array, Arr: f.arr}
			}
			stack = stack[:len(stack)-1]
		}
	}
}

func (r *reader) literal(word string) bool {
	if strings.HasPrefix(string(r.text[r.pos:min(len(r.text), r.pos+len(word))]), word) {
		r.pos += len(word)
		return true
	}
	return false
}

func digits(text []byte, pos int) int {
	for pos < len(text) && text[pos] >= '0' && text[pos] <= '9' {
		pos++
	}
	return pos
}

// number reads one number token of RFC 8259. In the Strict mode, a number outside I-JSON is counted.
func (r *reader) number() (x float64, ok, lossy bool) {
	start, pos := r.pos, r.pos
	if r.text[pos] == '-' {
		pos++
	}
	switch {
	case pos < len(r.text) && r.text[pos] == '0':
		pos++
	case pos < len(r.text) && r.text[pos] >= '1' && r.text[pos] <= '9':
		pos = digits(r.text, pos)
	default:
		return 0, false, false
	}
	if pos < len(r.text) && r.text[pos] == '.' {
		if end := digits(r.text, pos+1); end > pos+1 {
			pos = end
		}
	}
	if pos < len(r.text) && (r.text[pos] == 'e' || r.text[pos] == 'E') {
		at := pos + 1
		if at < len(r.text) && (r.text[at] == '+' || r.text[at] == '-') {
			at++
		}
		if end := digits(r.text, at); end > at {
			pos = end
		}
	}
	r.pos = pos
	token := string(r.text[start:pos])
	x, kept := Kept(token)
	if !kept {
		if r.mode == Strict {
			r.outside++
			return 0, true, false
		}
		x, _ = strconv.ParseFloat(token, 64)
	}
	if x == 0 {
		x = 0 // -0 has the canonical form 0
	}
	return x, true, !kept
}

// hex4 reads four hexadecimal digits at a position.
func hex4(text []byte, pos int) (rune, bool) {
	if pos+4 > len(text) {
		return 0, false
	}
	var u rune
	for _, c := range text[pos : pos+4] {
		switch {
		case c >= '0' && c <= '9':
			u = u<<4 | rune(c-'0')
		case c >= 'a' && c <= 'f':
			u = u<<4 | rune(c-'a'+10)
		case c >= 'A' && c <= 'F':
			u = u<<4 | rune(c-'A'+10)
		default:
			return 0, false
		}
	}
	return u, true
}

// appendSurrogate adds a lone surrogate in the generalized form of UTF-8 (three bytes, ED A0 80 to ED BF BF).
func appendSurrogate(b []byte, u rune) []byte {
	return append(b, 0xE0|byte(u>>12), 0x80|byte(u>>6)&0x3F, 0x80|byte(u)&0x3F)
}

func isSurrogateBytes(b []byte) bool {
	return len(b) >= 3 && b[0] == 0xED && b[1] >= 0xA0 && b[1] <= 0xBF && b[2] >= 0x80 && b[2] <= 0xBF
}

// str reads one string token. The position is at its first quotation mark. In the Strict mode, a string
// outside I-JSON is counted. In each mode the result keeps a lone surrogate in its three-byte form.
func (r *reader) str() (string, bool) {
	text := r.text
	pos := r.pos + 1
	start := pos
	var out []byte // nil while the token has no escape and no change
	valid := true
	for {
		if pos >= len(text) {
			return "", false
		}
		c := text[pos]
		switch {
		case c == '"':
			var s string
			if out == nil {
				s = string(text[start:pos])
			} else {
				s = string(out)
			}
			r.pos = pos + 1
			if valid && !ValidString(s) {
				valid = false // a noncharacter
			}
			if !valid && r.mode == Strict {
				r.outside++
			}
			return s, true
		case c < 0x20:
			return "", false
		case c == '\\':
			if out == nil {
				out = append(make([]byte, 0, pos-start+16), text[start:pos]...)
			}
			pos++
			if pos >= len(text) {
				return "", false
			}
			switch text[pos] {
			case '"', '\\', '/':
				out = append(out, text[pos])
			case 'b':
				out = append(out, '\b')
			case 'f':
				out = append(out, '\f')
			case 'n':
				out = append(out, '\n')
			case 'r':
				out = append(out, '\r')
			case 't':
				out = append(out, '\t')
			case 'u':
				u, ok := hex4(text, pos+1)
				if !ok {
					return "", false
				}
				pos += 4
				if u >= 0xD800 && u <= 0xDBFF && pos+2 < len(text) && text[pos+1] == '\\' && text[pos+2] == 'u' {
					if low, ok := hex4(text, pos+3); ok && low >= 0xDC00 && low <= 0xDFFF {
						u = 0x10000 + (u-0xD800)<<10 + (low - 0xDC00)
						pos += 6
					}
				}
				if u >= 0xD800 && u <= 0xDFFF {
					valid = false
					out = appendSurrogate(out, u)
				} else {
					out = utf8.AppendRune(out, u)
				}
			default:
				return "", false
			}
			pos++
		case c < 0x80:
			if out != nil {
				out = append(out, c)
			}
			pos++
		default:
			_, size := utf8.DecodeRune(text[pos:])
			if size <= 1 {
				if !r.inner || !isSurrogateBytes(text[pos:]) {
					return "", false // invalid UTF-8
				}
				valid = false
				size = 3
			}
			if out != nil {
				out = append(out, text[pos:pos+size]...)
			}
			pos += size
		}
	}
}

// Unquote returns the bytes of one JSON string token, for example a descriptor text in a request. A lone
// surrogate escape stays in its three-byte form, so ParseInner can report it. The result is false if the
// token is not one JSON string.
func Unquote(token []byte) ([]byte, bool) {
	r := &reader{text: token, mode: Lenient}
	r.space()
	if r.peek() != '"' {
		return nil, false
	}
	s, ok := r.str()
	if !ok {
		return nil, false
	}
	r.space()
	return []byte(s), r.pos == len(token)
}

// decimal is the exact value of a number token: sign, digits with no zero at each end, and a power of ten.
type decimal struct {
	neg    bool
	digits string // empty for zero
	exp    int64
	huge   bool // the exponent does not fit; the value is not zero
}

func normal(token string) decimal {
	var d decimal
	if strings.HasPrefix(token, "-") {
		d.neg = true
		token = token[1:]
	}
	mantissa, exponent := token, ""
	if i := strings.IndexAny(token, "eE"); i >= 0 {
		mantissa, exponent = token[:i], token[i+1:]
	}
	whole, frac, _ := strings.Cut(mantissa, ".")
	all := strings.TrimLeft(whole+frac, "0")
	trimmed := strings.TrimRight(all, "0")
	if trimmed == "" {
		return decimal{}
	}
	d.digits = trimmed
	d.exp = int64(len(all)-len(trimmed)) - int64(len(frac))
	if exponent != "" {
		sign := int64(1)
		switch exponent[0] {
		case '-':
			sign = -1
			exponent = exponent[1:]
		case '+':
			exponent = exponent[1:]
		}
		exponent = strings.TrimLeft(exponent, "0")
		if len(exponent) > 15 {
			d.huge = true
			return d
		}
		if exponent != "" {
			e, _ := strconv.ParseInt(exponent, 10, 64)
			d.exp += sign * e
		}
	}
	return d
}

// Kept tells if a number token is inside I-JSON as STS-MAL-3 states: it is finite as an IEEE 754 double, and
// its canonical form (IDN-CAN-1) has the same value. The notation has no effect. It returns the double.
func Kept(token string) (float64, bool) {
	x, err := strconv.ParseFloat(token, 64)
	if err != nil || math.IsInf(x, 0) || math.IsNaN(x) {
		return 0, false
	}
	a, b := normal(token), normal(ESNumber(x))
	if a.huge || b.huge {
		return 0, false // a value that is not zero, with a double of zero
	}
	if a.digits == "" || b.digits == "" {
		return x, a.digits == b.digits
	}
	return x, a.neg == b.neg && a.digits == b.digits && a.exp == b.exp
}

// ESNumber returns the ECMAScript form of a finite number (RFC 8785, section 3.2.2.3).
func ESNumber(x float64) string {
	if x == 0 {
		return "0"
	}
	if x < 0 {
		return "-" + ESNumber(-x)
	}
	text := strconv.FormatFloat(x, 'e', -1, 64) // the shortest digits that give x again
	mantissa, exponent, _ := strings.Cut(text, "e")
	d := strings.Replace(mantissa, ".", "", 1)
	e, _ := strconv.Atoi(exponent)
	k, n := len(d), e+1
	switch {
	case k <= n && n <= 21:
		return d + strings.Repeat("0", n-k)
	case 0 < n && n <= 21:
		return d[:n] + "." + d[n:]
	case -6 < n && n <= 0:
		return "0." + strings.Repeat("0", -n) + d
	}
	sign := "+"
	if n-1 < 0 {
		sign = "-"
		e = -(n - 1)
	} else {
		e = n - 1
	}
	if k == 1 {
		return d + "e" + sign + strconv.Itoa(e)
	}
	return d[:1] + "." + d[1:] + "e" + sign + strconv.Itoa(e)
}

// IsIJSON tells if a value of a request agrees with I-JSON (RFC 7493), with no recursion: each string and
// each key passes ValidString, and no number is outside I-JSON. A value from the Strict mode always agrees.
func IsIJSON(v Value) bool {
	todo := []Value{v}
	for len(todo) > 0 {
		v = todo[len(todo)-1]
		todo = todo[:len(todo)-1]
		switch v.Kind {
		case String:
			if !ValidString(v.Str) {
				return false
			}
		case Number:
			if v.Lossy || math.IsInf(v.Num, 0) || math.IsNaN(v.Num) {
				return false
			}
		case Array:
			todo = append(todo, v.Arr...)
		case Object:
			for i := range v.Obj {
				if !ValidString(v.Obj[i].Key) {
					return false
				}
				todo = append(todo, v.Obj[i].Val)
			}
		}
	}
	return true
}

// Text returns the string of a string value.
func (v Value) Text() (string, bool) {
	return v.Str, v.Kind == String
}

// Int returns the value of a number that is an exact integer in the range of int64.
func (v Value) Int() (int64, bool) {
	if !v.IsInt() || v.Lossy || v.Num < -(1<<63) || v.Num >= 1<<63 {
		return 0, false
	}
	return int64(v.Num), true
}

// Keys returns the keys of an object in the order of the text.
func (v Value) Keys() []string {
	out := make([]string, len(v.Obj))
	for i := range v.Obj {
		out[i] = v.Obj[i].Key
	}
	return out
}
