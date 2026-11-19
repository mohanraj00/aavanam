// Package canon makes the canonical form of a JSON value (RFC 8785) and its digest (IDN-CAN-1, IDN-CAN-2).
package canon

import (
	"sort"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/mohanraj00/aavanam/backend/core/jsontext"
)

const hexDigits = "0123456789abcdef"

// AppendString adds the JSON form of a string: only `"`, `\` and the control characters below U+0020 have
// an escape (RFC 8785, section 3.2.2.2). A lone surrogate in its three-byte form gets a `\u` escape, and
// each other byte sequence that is not UTF-8 becomes U+FFFD. A stored string has neither (KND-SCH-4).
func AppendString(b []byte, s string) []byte {
	b = append(b, '"')
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '"' || c == '\\':
			b = append(b, '\\', c)
			i++
		case c >= 0x20 && c < 0x80:
			b = append(b, c)
			i++
		case c < 0x20:
			switch c {
			case '\b':
				b = append(b, '\\', 'b')
			case '\f':
				b = append(b, '\\', 'f')
			case '\n':
				b = append(b, '\\', 'n')
			case '\r':
				b = append(b, '\\', 'r')
			case '\t':
				b = append(b, '\\', 't')
			default:
				b = append(b, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xF])
			}
			i++
		default:
			r, size := utf8.DecodeRuneInString(s[i:])
			switch {
			case r != utf8.RuneError || size > 1:
				b = append(b, s[i:i+size]...)
			case i+2 < len(s) && c == 0xED && s[i+1] >= 0xA0 && s[i+1] <= 0xBF && s[i+2] >= 0x80 && s[i+2] <= 0xBF:
				u := rune(c&0x0F)<<12 | rune(s[i+1]&0x3F)<<6 | rune(s[i+2]&0x3F)
				b = append(b, '\\', 'u', hexDigits[u>>12], hexDigits[u>>8&0xF], hexDigits[u>>4&0xF], hexDigits[u&0xF])
				size = 3
			default:
				b = utf8.AppendRune(b, utf8.RuneError)
			}
			i += size
		}
	}
	return append(b, '"')
}

// less orders two keys by their UTF-16 code units (RFC 8785, section 3.2.3).
func less(a, b string) bool {
	x, y := utf16.Encode([]rune(a)), utf16.Encode([]rune(b))
	for i := 0; i < len(x) && i < len(y); i++ {
		if x[i] != y[i] {
			return x[i] < y[i]
		}
	}
	return len(x) < len(y)
}

func ascii(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func sorted(members []jsontext.Member) []jsontext.Member {
	plain := true
	for i := range members {
		if !ascii(members[i].Key) {
			plain = false
			break
		}
	}
	out := append([]jsontext.Member(nil), members...)
	if plain { // the byte order of ASCII keys is the order of their code units
		sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	} else {
		sort.Slice(out, func(i, j int) bool { return less(out[i].Key, out[j].Key) })
	}
	return out
}

type step struct {
	value jsontext.Value
	token string // a part of the text that is already encoded, if not empty
}

// Append adds the canonical form of a value, with no recursion.
func Append(b []byte, v jsontext.Value) []byte { return appendValue(b, v, true) }

// Ordered returns the JSON text of a value with the members of each object in the order of the value. Two
// values of a list are the same value of a set if they have the same text (KND-SCH-5).
func Ordered(v jsontext.Value) []byte { return appendValue(nil, v, false) }

func appendValue(b []byte, v jsontext.Value, sortKeys bool) []byte {
	todo := []step{{value: v}}
	for len(todo) > 0 {
		s := todo[len(todo)-1]
		todo = todo[:len(todo)-1]
		if s.token != "" {
			b = append(b, s.token...)
			continue
		}
		switch s.value.Kind {
		case jsontext.Null:
			b = append(b, "null"...)
		case jsontext.Bool:
			if s.value.Bool {
				b = append(b, "true"...)
			} else {
				b = append(b, "false"...)
			}
		case jsontext.Number:
			b = append(b, jsontext.ESNumber(s.value.Num)...)
		case jsontext.String:
			b = AppendString(b, s.value.Str)
		case jsontext.Array:
			b = append(b, '[')
			todo = append(todo, step{token: "]"})
			for i := len(s.value.Arr) - 1; i >= 0; i-- {
				todo = append(todo, step{value: s.value.Arr[i]})
				if i > 0 {
					todo = append(todo, step{token: ","})
				}
			}
		case jsontext.Object:
			b = append(b, '{')
			todo = append(todo, step{token: "}"})
			members := s.value.Obj
			if sortKeys {
				members = sorted(members)
			}
			for i := len(members) - 1; i >= 0; i-- {
				todo = append(todo, step{value: members[i].Val})
				key := string(AppendString(nil, members[i].Key)) + ":"
				if i > 0 {
					key = "," + key
				}
				todo = append(todo, step{token: key})
			}
		}
	}
	return b
}

// Canonical returns the canonical form of a value in UTF-8.
func Canonical(v jsontext.Value) []byte { return Append(nil, v) }

// Revision returns the digest of the canonical form (IDN-CAN-2).
func Revision(v jsontext.Value) string { return jsontext.Digest(Canonical(v)) }
