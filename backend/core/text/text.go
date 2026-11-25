package text

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"unicode/utf8"

	"github.com/mohanraj00/aavanam/backend/core/check"
	"github.com/mohanraj00/aavanam/backend/core/envelope"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/ops"
)

// Word is one word of payload bytes: its start, its end and its folded form (CRD-TXT-3).
type Word struct {
	Start, End int
	Folded     string
}

// isWordByte tells if a byte is a word byte: A to Z, a to z, 0 to 9, or 0x80 to 0xFF (CRD-TXT-3 rule 1).
func isWordByte(b byte) bool {
	return b >= 0x80 || ('0' <= b && b <= '9') || ('A' <= b && b <= 'Z') || ('a' <= b && b <= 'z')
}

// Words returns each word of the bytes: a maximal sequence of word bytes. The folded form changes only A to
// Z into a to z. The rules apply to bytes and do not decode UTF-8 (CRD-TXT-3).
func Words(data []byte) []Word {
	var out []Word
	for i := 0; i < len(data); {
		if !isWordByte(data[i]) {
			i++
			continue
		}
		start := i
		for i < len(data) && isWordByte(data[i]) {
			i++
		}
		folded := make([]byte, i-start)
		for n, b := range data[start:i] {
			if 'A' <= b && b <= 'Z' {
				b += 'a' - 'A'
			}
			folded[n] = b
		}
		out = append(out, Word{start, i, string(folded)})
	}
	return out
}

// Token returns the index token of a folded word: the letter `t` and the first 16 hexadecimal digits of the
// SHA-256 value of the folded form, in lowercase (DEV-FTS-3). Equal words give equal tokens.
func Token(folded string) string {
	sum := sha256.Sum256([]byte(folded))
	return "t" + hex.EncodeToString(sum[:8])
}

// Tokens returns the token of each word, in the word order.
func Tokens(words []Word) []string {
	out := make([]string, len(words))
	for i, w := range words {
		out[i] = Token(w.Folded)
	}
	return out
}

// IsQuery checks a query of CRD-TXT-1: 1 to 1024 Unicode scalar values, with 1 to 32 words.
func IsQuery(v jsontext.Value) bool {
	if v.Kind != jsontext.String || !utf8.ValidString(v.Str) { // no surrogate code point
		return false
	}
	if n := utf8.RuneCountInString(v.Str); n < 1 || n > 1024 {
		return false
	}
	n := len(Words([]byte(v.Str)))
	return 1 <= n && n <= 32
}

// IsTextMedia applies CRD-TXT-2 to a media type: the part before the first `;`, without ASCII spaces at its
// ends, with A to Z folded, is `text/` and one or more more characters.
func IsTextMedia(media string) bool {
	part := []byte(media)
	for i, b := range part {
		if b == ';' {
			part = part[:i]
			break
		}
	}
	for len(part) > 0 && part[0] == ' ' {
		part = part[1:]
	}
	for len(part) > 0 && part[len(part)-1] == ' ' {
		part = part[:len(part)-1]
	}
	if len(part) <= len("text/") {
		return false
	}
	for i, want := range []byte("text/") {
		b := part[i]
		if 'A' <= b && b <= 'Z' {
			b += 'a' - 'A'
		}
		if b != want {
			return false
		}
	}
	return true
}

// textPayload returns the payload of an artifact with a text media type (CRD-TXT-2).
func textPayload(desc jsontext.Value) (check.Payload, bool) {
	if check.Kind(desc) != "artifact" {
		return check.Payload{}, false
	}
	payload, ok := check.PayloadOf(desc, "payload")
	return payload, ok && IsTextMedia(payload.MediaType)
}

// Index gives the tokens of the bytes of a text payload to a storage that has a text index (CRD-TXT-3,
// DEV-FTS-3). A storage with no text index needs no tokens.
func Index(tx ops.Tx, desc jsontext.Value, digest string, data []byte) {
	index, ok := tx.(ops.TextIndex)
	if !ok {
		return
	}
	if payload, ok := textPayload(desc); ok && payload.Digest == digest && !index.TextIndexed(digest) {
		index.PutTextTokens(digest, Tokens(Words(data)))
	}
}

// SearchText is the operation `search_text` (CRD-TXT-1 to CRD-TXT-6, CRD-ORD-1 to CRD-ORD-3).
func SearchText(c *ops.Call) envelope.Envelope {
	c.Strict([]ops.Field{ops.Req("context", ops.IsStr), ops.Req("query", IsQuery), ops.Opt("offset", ops.IsCount(0)),
		ops.Opt("limit", ops.IsCount(1))}, nil, nil) // CRD-TXT-1 steps 1 to 3
	rc := c.Context() // steps 4 and 5
	queryValue, _ := c.Request.Get("query")
	query := Words([]byte(queryValue.Str))
	// CRD-TXT-6 rule 2: the snapshot roots of the context, and the closure of each one. The call reads a
	// closure only for an item. The closure of the only root of a context is the content of the context.
	var roots []string
	for _, root := range rc.Roots {
		if c.Kind(root) == "snapshot" && !contains(roots, root) {
			roots = append(roots, root)
		}
	}
	sort.Strings(roots)
	members := rc.Members() // CRD-TXT-2, CRD-ORD-1: ascending ref
	var closures map[string]map[string]struct{}
	inSnapshots := func(ref string) []string {
		if closures == nil {
			closures = map[string]map[string]struct{}{}
			for _, root := range roots {
				if len(rc.Roots) == 1 {
					closures[root] = make(map[string]struct{}, len(members))
					for _, member := range members {
						closures[root][member] = struct{}{}
					}
				} else {
					closures[root] = c.Closure(root)
				}
			}
		}
		var in []string
		for _, root := range roots {
			if _, ok := closures[root][ref]; ok {
				in = append(in, root)
			}
		}
		return in
	}
	// The candidates come from the members of the context, not from the index (CRD-TXT-5, DEV-FTS-5).
	type candidate struct{ ref, digest string }
	var candidates []candidate
	var refs, digests []string
	seen := map[string]bool{}
	c.Preload(members)
	for _, ref := range members {
		desc, ok := c.Descriptor(ref)
		if !ok {
			continue
		}
		payload, ok := textPayload(desc)
		if !ok {
			continue
		}
		candidates = append(candidates, candidate{ref, payload.Digest})
		refs = append(refs, ref)
		if !seen[payload.Digest] {
			seen[payload.Digest] = true
			digests = append(digests, payload.Digest)
		}
	}
	// The index only selects the digests to scan (CRD-TXT-3, DEV-FTS-5). A digest that the index does not
	// have is scanned, as with no index (DEV-OUT-3).
	index, indexed := c.S.(ops.TextIndex)
	possible, unindexed := map[string]bool{}, map[string]bool{}
	if indexed && len(digests) > 0 {
		can, not := index.TextCandidates(Tokens(query), digests)
		for _, digest := range can {
			possible[digest] = true
		}
		for _, digest := range not {
			unindexed[digest] = true
		}
	}
	deferred := c.Deferred(refs)
	fill := map[string][]string{}
	var items []envelope.Item
	for _, x := range candidates {
		scan := !indexed || unindexed[x.digest] || possible[x.digest]
		if !scan && !deferred[x.ref] {
			// The index has the digest, so the store has its bytes. The descriptor has no deferred record.
			// Thus the bytes are available (BUN-IMP-12), and they have no match.
			continue
		}
		data, ok := c.BlobOf(x.digest, x.ref, deferred[x.ref]) // BUN-IMP-12 before the index
		if !ok {
			items = append(items, envelope.Refused([]string{"payload_missing"}, x.ref)) // CRD-TXT-5
			continue
		}
		if !scan {
			continue
		}
		words := Words(data)
		if indexed && unindexed[x.digest] {
			fill[x.digest] = Tokens(words)
		}
		var in []string
		for n, match := range Matches(words, query) {
			if n == 0 {
				in = inSnapshots(x.ref)
			}
			selector := jsontext.O(jsontext.M("selector_profile", jsontext.S(check.Bytes)),
				jsontext.M("start", jsontext.N(float64(match[0]))), jsontext.M("end", jsontext.N(float64(match[1]))))
			items = append(items, envelope.OK(jsontext.M("ref", jsontext.S(x.ref)), jsontext.M("snapshots", jsontext.Strings(in)),
				jsontext.M("selector", selector)))
		}
	}
	if len(fill) > 0 {
		// A store that got its text payloads before it had the index has no tokens for them. The scan of
		// such a digest gives its tokens to the index, so the next search does not read its bytes.
		c.S.Update(func(tx ops.Tx) {
			if w, ok := tx.(ops.TextIndex); ok {
				for _, digest := range digests {
					if tokens, ok := fill[digest]; ok {
						w.PutTextTokens(digest, tokens)
					}
				}
			}
		})
	}
	offset, limit := int64(0), int64(-1)
	if v, ok := c.Request.Get("offset"); ok {
		offset, _ = v.Int()
	}
	if v, ok := c.Request.Get("limit"); ok {
		limit, _ = v.Int()
	}
	if offset > int64(len(items)) {
		offset = int64(len(items))
	}
	rest := items[offset:]
	if limit >= 0 && int64(len(rest)) > limit { // CRD-ORD-2
		return c.Bounded(rest[:limit]).WithContext(rc.ID)
	}
	return c.Result(rest).WithContext(rc.ID)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// Matches returns the start and the end of each match of the query words in the words: each word start
// where the folded words agree with the query, so matches can overlap (CRD-TXT-4).
func Matches(words, query []Word) [][2]int {
	var out [][2]int
	n := len(query)
	for i := 0; i+n <= len(words); i++ {
		match := true
		for k := 0; k < n && match; k++ {
			match = words[i+k].Folded == query[k].Folded
		}
		if match {
			out = append(out, [2]int{words[i].Start, words[i+n-1].End})
		}
	}
	return out
}
