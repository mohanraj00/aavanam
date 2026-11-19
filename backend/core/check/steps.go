package check

import (
	"bytes"
	"sort"

	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/kinds"
)

// Env gives the steps of STS-MAL-3 that need other objects their view of the batch and the store.
type Env struct {
	// Lookup returns the normal descriptor of a reference in the batch or in the store.
	Lookup func(ref string) (jsontext.Value, bool)
	// Data returns the bytes of a digest in the batch or in the store.
	Data func(digest string) ([]byte, bool)
	// Incomplete gives `bundle_incomplete` for an absent target or payload, as an import does (BUN-IMP-2).
	Incomplete bool
	// Withheld has the digests whose bytes a bundle declares as withheld (BUN-IMP-9).
	Withheld map[string]bool
}

func (e Env) absent() string {
	if e.Incomplete {
		return "bundle_incomplete"
	}
	return "payload_missing"
}

// References applies step 4 of STS-MAL-3: each target is present and has a permitted kind (KND-REF-2,
// KND-REF-3), and a supersession has a target and a replacement of one kind (REL-RET-2).
func (e Env) References(desc jsontext.Value) *Refusal {
	refs := References(desc)
	var missing, wrong []string
	for _, r := range refs {
		if _, ok := e.Lookup(r.Ref); !ok {
			missing = append(missing, r.Ref)
		}
	}
	if len(missing) > 0 {
		code := "missing_target"
		if e.Incomplete {
			code = "bundle_incomplete"
		}
		return refuse(code, len(missing), missing...)
	}
	kind := Kind(desc)
	for _, r := range refs {
		target, _ := e.Lookup(r.Ref)
		if f, _ := kinds.Find(kind, r.Field); !f.Permits(Kind(target)) {
			wrong = append(wrong, r.Ref)
		}
	}
	if len(wrong) > 0 {
		return refuse("unsupported_target_kind", len(wrong), wrong...)
	}
	if kind == "supersession" {
		target, _ := desc.Get("target")
		replacement, _ := desc.Get("replacement")
		a, _ := e.Lookup(target.Str)
		b, _ := e.Lookup(replacement.Str)
		if Kind(a) != Kind(b) {
			return refuse("replacement_kind", 1, replacement.Str)
		}
	}
	return nil
}

// Payloads applies step 5 of STS-MAL-3 to the payload bytes of a descriptor: present, with the declared
// digest and size (KND-OPQ-1). It defers only the absent bytes of a withheld digest (BUN-IMP-9).
func (e Env) Payloads(desc jsontext.Value) *Refusal {
	missing, digests, sizes := 0, 0, 0
	for _, p := range Payloads(desc) {
		data, ok := e.Data(p.Digest)
		switch {
		case !ok && e.Withheld[p.Digest]:
		case !ok:
			missing++
		case jsontext.Digest(data) != p.Digest:
			digests++
		case int64(len(data)) != p.Size:
			sizes++
		}
	}
	switch {
	case missing > 0:
		return refuse(e.absent(), missing)
	case digests > 0:
		return refuse("digest_mismatch", digests)
	case sizes > 0:
		return refuse("size_mismatch", sizes)
	}
	return nil
}

// Content applies the checks of step 6 of STS-MAL-3 that need no section: the bytes of an excerpt
// (BUN-EXC-3, BUN-EXC-4) and the native identities of a snapshot (IDN-NAT-3).
func (e Env) Content(desc jsontext.Value) *Refusal {
	switch Kind(desc) {
	case "excerpt":
		parent, _ := desc.Get("parent")
		reading, _ := e.Lookup(parent.Str)
		whole, _ := PayloadOf(reading, "payload")
		own, _ := PayloadOf(desc, "payload")
		data, hasWhole := e.Data(whole.Digest)
		part, hasPart := e.Data(own.Digest)
		if (!hasWhole && e.Withheld[whole.Digest]) || (!hasPart && e.Withheld[own.Digest]) {
			return nil // BUN-IMP-9: defer the check until the bytes are in the store
		}
		if !hasWhole {
			return refuse(e.absent(), 1)
		}
		ranges, _ := desc.Get("ranges")
		var joined []byte
		for _, r := range ranges.Arr {
			start, _ := r.Get("start")
			end, _ := r.Get("end")
			if end.Num > float64(len(data)) {
				return refuse("excerpt_mismatch", 1)
			}
			joined = append(joined, data[int(start.Num):int(end.Num)]...)
		}
		if !hasPart || !bytes.Equal(joined, part) {
			return refuse("excerpt_mismatch", 1)
		}
	case "snapshot":
		mappings, _ := desc.Get("native_mappings")
		subjects := map[[2]string]map[string]struct{}{}
		for _, m := range mappings.Arr {
			mapping, _ := e.Lookup(m.Str)
			scheme, _ := mapping.Get("native_scheme")
			id, _ := mapping.Get("native_id")
			subject, _ := mapping.Get("subject")
			key := [2]string{scheme.Str, id.Str}
			if subjects[key] == nil {
				subjects[key] = map[string]struct{}{}
			}
			subjects[key][subject.Str] = struct{}{}
		}
		conflicts := 0
		for _, set := range subjects {
			if len(set) > 1 {
				conflicts++
			}
		}
		if conflicts > 0 {
			return refuse("native_id_conflict", conflicts)
		}
	}
	return nil
}

// NotInServing has the kinds that a snapshot of a `serving` collection must not have in its closure (PUB-SEC-3).
var NotInServing = map[string]bool{"source": true, "reading": true, "excerpt": true, "evidence": true}

// SectionViolations returns, in ascending order, each descriptor in the closure of a snapshot that a
// `serving` collection does not permit (PUB-SEC-3). The caller tells if the collection is `serving`.
func (e Env) SectionViolations(desc jsontext.Value) []string {
	seen := map[string]struct{}{}
	var found, todo []string
	for _, r := range References(desc) {
		todo = append(todo, r.Ref)
	}
	for len(todo) > 0 {
		ref := todo[len(todo)-1]
		todo = todo[:len(todo)-1]
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		target, ok := e.Lookup(ref)
		if !ok {
			continue // a member of this batch that fails its own step 4 (STS-ENV-5)
		}
		if NotInServing[Kind(target)] {
			found = append(found, ref)
		}
		for _, r := range References(target) {
			todo = append(todo, r.Ref)
		}
	}
	sort.Strings(found)
	return found
}
