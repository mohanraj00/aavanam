package read

import (
	"crypto/sha256"
	"encoding/binary"
	"sort"

	"github.com/mohanraj00/aavanam/backend/core/check"
	"github.com/mohanraj00/aavanam/backend/core/envelope"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/kinds"
	"github.com/mohanraj00/aavanam/backend/core/ops"
	"github.com/mohanraj00/aavanam/backend/core/text"
)

// Stale returns the snapshot root of the read context that makes a retrieval index stale, or "" (IDX-STL-1):
// a root of the collection of the snapshot of the index that is a different snapshot. A read context has
// one such root at most (CTX-CMP-1).
func Stale(c *ops.Call, rc *ops.ReadContext, index string) string {
	desc, _ := c.Descriptor(index)
	snapshot := str(desc, "snapshot")
	collection := collectionOf(c, snapshot)
	roots := append([]string(nil), rc.Roots...)
	sort.Strings(roots)
	for _, root := range roots {
		if root != snapshot && c.Kind(root) == "snapshot" && collectionOf(c, root) == collection {
			return root
		}
	}
	return ""
}

// Buckets returns the bucket set of the terms of the bytes under the method `aavanam/hashed-terms/v1`, in
// ascending order (IDX-MTH-4). A term is the folded form of a word (CRD-TXT-3). Its bucket is the first
// four bytes of its SHA-256 digest as an unsigned integer, most significant byte first, modulo `dimensions`.
func Buckets(data []byte, dimensions uint32) []uint32 {
	seen := map[uint32]struct{}{}
	terms := map[string]struct{}{}
	for _, word := range text.Words(data) {
		if _, done := terms[word.Folded]; done {
			continue
		}
		terms[word.Folded] = struct{}{}
		sum := sha256.Sum256([]byte(word.Folded))
		seen[binary.BigEndian.Uint32(sum[:4])%dimensions] = struct{}{}
	}
	out := make([]uint32, 0, len(seen))
	for bucket := range seen {
		out = append(out, bucket)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// unitBuckets returns the bucket set of one item of an index: the buckets of each text of the item
// (IDX-MTH-3, IDX-MTH-4). The texts are the payloads of each artifact with a text media type in the closure
// of the item. If the bytes of a text are not available, the item has no bucket set (IDX-SRC-4, BUN-IMP-12).
func unitBuckets(c *ops.Call, unit string, dimensions uint32) (map[uint32]struct{}, bool) {
	out := map[uint32]struct{}{}
	seen := map[string]struct{}{unit: {}}
	todo := []string{unit}
	for len(todo) > 0 {
		ref := todo[len(todo)-1]
		todo = todo[:len(todo)-1]
		desc, _ := c.Descriptor(ref)
		if payload, ok := check.PayloadOf(desc, "payload"); ok && check.Kind(desc) == "artifact" && text.IsTextMedia(payload.MediaType) {
			data, ok := c.Blob(payload.Digest, ref)
			if !ok {
				return nil, false
			}
			for _, bucket := range Buckets(data, dimensions) {
				out[bucket] = struct{}{}
			}
		}
		// The kept `refs` of the object (KND-DEP-2). The closure of an item keeps the references of a removed
		// object, so the texts of an item do not change after a removal or after a later `put` (REL-RMV-8).
		for _, target := range c.S.Targets(ref) {
			if _, ok := seen[target]; !ok {
				seen[target] = struct{}{}
				todo = append(todo, target)
			}
		}
	}
	return out, true
}

// scored is one candidate of a search and its order key (IDX-SRC-6).
type scored struct {
	score int
	noID  bool
	id    string
	ref   string
}

// SemanticSearch is the operation `semantic_search` of the retrieval index profile (IDX-SRC-1 to IDX-SRC-7).
// The index is a projection of its snapshot. The store keeps no derived data for it, so each search makes
// the bucket sets again from the snapshot (IDX-DEF-2). The store does not load or run a method: the method
// `aavanam/hashed-terms/v1` is code of this module, and each other method gives `unsupported_profile`
// (IDX-MTH-2).
func SemanticSearch(c *ops.Call) envelope.Envelope {
	c.Strict([]ops.Field{ops.Req("context", ops.IsStr), ops.Req("index", ops.IsRef), ops.Req("query", text.IsQuery),
		ops.Opt("offset", ops.IsCount(0)), ops.Opt("limit", ops.IsCount(1)), ops.Opt("filter", IsFilter)}, nil, nil) // IDX-SRC-1 steps 1 to 3
	rc := c.Context() // steps 4 and 5
	index := str(c.Request, "index")
	stop := func(code string, refs ...string) { panic(envelope.FailRefs(refs, code)) }
	if code := rc.Locate(index); code != "" { // IDX-SRC-2
		stop(code, index)
	}
	desc, _ := c.Descriptor(index)
	if check.Kind(desc) != "retrieval-index" {
		stop("unsupported_target_kind", index)
	}
	if root := Stale(c, rc, index); root != "" {
		stop("index_stale", index, root) // IDX-STL-1
	}
	if method := str(desc, "method"); method != kinds.HashedTerms {
		ops.FailWith("unsupported", envelope.Reason{Code: "unsupported_profile", Refs: []string{index}, Capabilities: []string{method}})
	}
	dimensions := uint32(field(field(desc, "parameters"), "dimensions").Num)
	snapshot := str(desc, "snapshot")
	wanted := Buckets([]byte(str(c.Request, "query")), dimensions)
	snapshotDesc, _ := c.Descriptor(snapshot)
	units := ops.Strings(field(snapshotDesc, "units")) // IDX-MTH-3: the items of the index
	c.Preload(ops.Sorted(c.Closure(snapshot)))
	var found []scored
	var absent []string
	for _, unit := range units {
		buckets, ok := unitBuckets(c, unit, dimensions)
		if !ok {
			absent = append(absent, unit) // IDX-SRC-4: no score from a part of the texts
			continue
		}
		score := 0
		for _, bucket := range wanted {
			if _, has := buckets[bucket]; has {
				score++
			}
		}
		if score == 0 {
			continue // IDX-MTH-5: an item with the score 0 does not match
		}
		unitDesc, _ := c.Descriptor(unit)
		id, hasID := unitDesc.Get("logical_id")
		found = append(found, scored{score, !hasID, id.Str, unit})
	}
	sort.Slice(found, func(i, j int) bool { // IDX-SRC-6
		a, b := found[i], found[j]
		switch {
		case a.score != b.score:
			return a.score > b.score
		case a.noID != b.noID:
			return !a.noID
		case a.id != b.id:
			return a.id < b.id
		}
		return a.ref < b.ref
	})
	entries := make([]envelope.Item, len(found))
	refs := make([]string, len(found))
	for n, x := range found { // IDX-SRC-3
		entries[n] = item(m("ref", jsontext.S(x.ref)), m("kind", jsontext.S("knowledge-unit")), m("logical_id", nullableID(x)),
			m("snapshot", jsontext.S(snapshot)), m("index", jsontext.S(index)), m("score", num(int64(x.score))))
		refs[n] = x.ref
	}
	if filter, ok := c.Request.Get("filter"); ok { // IDX-SRC-5
		kept := entries[:0:0]
		for n, o := range Filtered(c, filter, refs, rc.Members()) {
			switch {
			case o.Failed != nil:
				kept = append(kept, *o.Failed)
			case o.Match:
				kept = append(kept, entries[n])
			}
		}
		entries = kept
	}
	sort.Strings(absent)
	for _, unit := range absent {
		entries = append(entries, envelope.Refused([]string{"payload_missing"}, unit))
	}
	items, bounded := Page(c, entries)
	return result(c, rc, items, bounded)
}

// nullableID returns the logical ID of a candidate, or null.
func nullableID(x scored) jsontext.Value {
	if x.noID {
		return jsontext.Value{}
	}
	return jsontext.S(x.id)
}
