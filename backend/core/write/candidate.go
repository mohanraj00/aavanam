package write

import (
	"sort"

	"github.com/mohanraj00/aavanam/backend/core/canon"
	"github.com/mohanraj00/aavanam/backend/core/envelope"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/ops"
)

// isHead tells if the value is the reference of a head, or null for no head (PUB-CND-1).
func isHead(v jsontext.Value) bool { return v.Kind == jsontext.Null || ops.IsRef(v) }

// optional returns a reference as a JSON value: null for no reference.
func optional(ref string) jsontext.Value {
	if ref == "" {
		return jsontext.Value{}
	}
	return jsontext.S(ref)
}

// each returns one reason with the code for each reference.
func each(code string, refs []string) []envelope.Reason {
	out := make([]envelope.Reason, len(refs))
	for i, ref := range refs {
		out[i] = envelope.R(code, ref)
	}
	return out
}

// head returns the head of a collection, or "" for no head (PUB-HED-1).
func head(r ops.Reader, collection string) string {
	return r.Heads([]string{collection})[collection]
}

// CreateCandidate is the operation `create_candidate` (PUB-CND-1 to PUB-CND-5). A candidate is a catalog
// record. The store does not run a check and does not read its outcome.
func CreateCandidate(c *ops.Call) envelope.Envelope {
	c.Strict([]ops.Field{ops.Req("snapshot", ops.IsRef), ops.Opt("checks", ops.ListOf(ops.IsRef, true)),
		ops.Req("policy", ops.IsStr), ops.Req("expected_head", isHead)}, []string{"checks"}, nil) // PUB-CND-1
	snapshot, _ := c.Request.Get("snapshot")
	list, _ := c.Request.Get("checks")
	policy, _ := c.Request.Get("policy")
	expected, _ := c.Request.Get("expected_head")
	checks := ops.Strings(list)
	sort.Strings(checks)
	one := func(status string, reasons []envelope.Reason) envelope.Envelope {
		return c.Result([]envelope.Item{{Status: status, Reasons: reasons}})
	}

	var unknown, wrong []string
	for i, ref := range append([]string{snapshot.Str}, checks...) {
		kind := c.Kind(ref)
		switch {
		case kind == "":
			unknown = append(unknown, ref)
		case i == 0 && kind != "snapshot", i > 0 && kind != "check":
			wrong = append(wrong, ref)
		}
	}
	if len(unknown) > 0 { // PUB-CND-2 step 1
		return one("not_found", each("unknown_ref", unknown))
	}
	if len(wrong) > 0 { // step 2
		return one("unsupported", each("unsupported_target_kind", wrong))
	}
	if _, registered := c.Store.Policy(policy.Str); !registered { // step 3
		return one("unsupported", []envelope.Reason{envelope.R("unsupported_profile")})
	}
	closure := c.Closure(snapshot.Str)
	var outside []string
	for _, ref := range checks {
		desc, _ := c.Descriptor(ref)
		subject, _ := desc.Get("subject")
		if _, in := closure[subject.Str]; !in {
			outside = append(outside, ref)
		}
	}
	if len(outside) > 0 { // step 4
		return one("incompatible", each("check_outside_snapshot", outside))
	}

	identity := canon.Revision(jsontext.O(jsontext.M("format", jsontext.S("aavanam-candidate/v1")), jsontext.M("snapshot", snapshot),
		jsontext.M("checks", jsontext.Strings(checks)), jsontext.M("policy", policy), jsontext.M("expected_head", expected))) // PUB-CND-3
	desc, _ := c.Descriptor(snapshot.Str)
	name, _ := desc.Get("collection")
	moved := false
	c.S.Update(func(tx ops.Tx) {
		tx.LockCollections([]string{name.Str})
		if _, has := tx.Candidate(identity); has { // step 5, PUB-CND-4
			return
		}
		if head(tx, name.Str) != expected.Str { // step 6
			moved = true
			return
		}
		tx.PutCandidate(ops.Candidate{ID: identity, Snapshot: snapshot.Str, Collection: name.Str, Policy: policy.Str,
			ExpectedHead: expected.Str})
	})
	if moved {
		return one("incompatible", []envelope.Reason{envelope.R("head_moved")})
	}
	return c.Result([]envelope.Item{envelope.OK(jsontext.M("candidate", jsontext.S(identity)), jsontext.M("collection", name))})
}
