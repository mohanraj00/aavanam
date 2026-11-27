package transfer

import (
	"sort"

	"github.com/mohanraj00/aavanam/backend/core/canon"
	"github.com/mohanraj00/aavanam/backend/core/check"
	"github.com/mohanraj00/aavanam/backend/core/envelope"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/kinds"
	"github.com/mohanraj00/aavanam/backend/core/ops"
	"github.com/mohanraj00/aavanam/backend/core/read"
)

// base is the descriptor profile of a lock.
const base = "aavanam/base/v1"

// lockKinds has the kind of the lock of each lock operation (LCK-EXE-4, LCK-BLD-3).
var lockKinds = map[string]string{"execution_inputs": "execution-lock", "build_inputs": "build-lock"}

// fail stops the operation with a status and its reasons.
func fail(status string, reasons []envelope.Reason) {
	panic(&envelope.Failure{Status: status, Reasons: reasons})
}

// permits tells if a field of a lock kind permits a target of a kind.
func permits(kind, name, target string) bool {
	f, _ := kinds.Find(kind, name)
	return f.Permits(target)
}

// target is one reference of a lock request and the kind and the field that give its permitted kinds.
type target struct{ ref, kind, field string }

// checkedRefs applies LCK-EXE-1, LCK-EXE-2, LCK-BLD-1 and LCK-BLD-2: each reference is in the read context,
// and then each reference has a permitted kind.
func checkedRefs(c *ops.Call, rc *ops.ReadContext, refs []target) {
	var located []envelope.Reason
	for _, r := range refs {
		if code := rc.Locate(r.ref); code != "" {
			located = append(located, envelope.R(code, r.ref))
		}
	}
	if len(located) > 0 {
		fail("not_found", located)
	}
	var wrong []envelope.Reason
	for _, r := range refs {
		if !permits(r.kind, r.field, c.Kind(r.ref)) {
			wrong = append(wrong, envelope.R("unsupported_target_kind", r.ref))
		}
	}
	if len(wrong) > 0 {
		fail("unsupported", wrong)
	}
}

// bindingsProblems applies LCK-EXE-3, BND-ROL-2 and BND-RUL-2, in this order.
func bindingsProblems(c *ops.Call, bindingSet jsontext.Value) {
	bindings, _ := bindingSet.Get("bindings")
	bound := func(role string) (string, bool) {
		v, ok := bindings.Get(role)
		return v.Str, ok
	}
	type artifact struct {
		ref  string
		desc jsontext.Value
	}
	var artifacts []artifact
	for _, mem := range bindings.Obj {
		if desc, ok := c.Descriptor(mem.Val.Str); ok && check.Kind(desc) == "artifact" {
			artifacts = append(artifacts, artifact{mem.Val.Str, desc})
		}
	}
	var unbound []envelope.Reason
	for _, a := range artifacts {
		roles, _ := a.desc.Get("declared_roles")
		for _, role := range roles.Obj {
			if _, ok := bound(role.Key); !ok {
				unbound = append(unbound, envelope.Reason{Code: "unbound_role"})
			}
		}
	}
	if len(unbound) > 0 {
		fail("unresolved", unbound)
	}
	var mismatch []envelope.Reason
	for _, a := range artifacts {
		roles, _ := a.desc.Get("declared_roles")
		for _, role := range roles.Obj {
			ref, _ := bound(role.Key)
			schema := role.Val.Str
			desc, _ := c.Descriptor(ref)
			kind := check.Kind(desc)
			own, _ := desc.Get("schema")
			if ref != schema && !((kind == "data-resource" || kind == "artifact") && own.Kind == jsontext.String && own.Str == schema) {
				mismatch = append(mismatch, envelope.R("role_schema_mismatch", ref))
			}
		}
	}
	if len(mismatch) > 0 {
		fail("incompatible", mismatch)
	}
	profiles := map[string]bool{}
	list, _ := bindingSet.Get("runtime_profiles")
	for _, p := range list.Arr {
		profiles[p.Str] = true
	}
	wrong := map[string]bool{}
	for _, a := range artifacts {
		if p, ok := a.desc.Get("runtime_profile"); ok && !profiles[p.Str] {
			wrong[a.ref] = true
		}
	}
	if len(wrong) > 0 {
		refs := make([]string, 0, len(wrong))
		for ref := range wrong {
			refs = append(refs, ref)
		}
		sort.Strings(refs)
		reasons := make([]envelope.Reason, len(refs))
		for n, ref := range refs {
			reasons[n] = envelope.R("runtime_profile_mismatch", ref)
		}
		fail("incompatible", reasons)
	}
}

// required applies LCK-CAP-2: one reason for each name of `requires` that is not in the capability table
// or not in the read context.
func required(c *ops.Call, rc *ops.ReadContext) {
	have := map[string]bool{}
	for _, name := range rc.Capabilities {
		have[name] = true
	}
	var reasons []envelope.Reason
	list, _ := c.Request.Get("requires")
	for _, name := range list.Arr {
		if have[name.Str] {
			continue
		}
		code := "capability_unavailable"
		if _, known := c.Store.Capabilities()[name.Str]; !known {
			code = "unsupported_profile"
		}
		reasons = append(reasons, envelope.Reason{Code: code, Capabilities: []string{name.Str}})
	}
	if len(reasons) > 0 {
		fail("unsupported", reasons)
	}
}

// indexes returns the form of the request field `indexes`, only for a store that declares the retrieval
// index profile (IDX-LCK-1).
func indexes(c *ops.Call) []ops.Field {
	if !c.Store.Profiles[kinds.IndexProfile] {
		return nil
	}
	return []ops.Field{ops.Opt("indexes", ops.ListOf(ops.IsRef, true))}
}

// indexTargets returns each index of a lock request with the kind and the field that give its permitted
// kind (IDX-LCK-2).
func indexTargets(c *ops.Call) []target {
	var out []target
	for _, r := range field(c.Request, "indexes").Arr {
		out = append(out, target{r.Str, lockKinds[c.Operation], "indexes"})
	}
	return out
}

// fresh applies the last step of IDX-LCK-2: one reason `index_stale` for each stale index of the request.
func fresh(c *ops.Call, rc *ops.ReadContext) {
	var reasons []envelope.Reason
	for _, r := range field(c.Request, "indexes").Arr {
		if root := read.Stale(c, rc, r.Str); root != "" {
			reasons = append(reasons, envelope.R("index_stale", r.Str, root))
		}
	}
	if len(reasons) > 0 {
		fail("incompatible", reasons)
	}
}

// lock writes the lock of the request and returns the envelope with the closure that the lock covers
// (LCK-EXE-4, LCK-BLD-3). A device store writes the lock to its local output store (BUN-IMP-7). In a
// layered context, the lock has the base snapshot and the layer in `view` (LYR-LCK-1). The lock has the
// `indexes` of the request (IDX-LCK-3). The covered set has no value of `view` or of `indexes`.
func lock(c *ops.Call, rc *ops.ReadContext, covered []string) envelope.Envelope {
	fields := []jsontext.Member{jsontext.M("descriptor_profile", jsontext.S(base)), jsontext.M("kind", jsontext.S(lockKinds[c.Operation]))}
	for _, mem := range c.Request.Obj {
		if mem.Key != "context" {
			fields = append(fields, mem)
		}
	}
	declared := c.Store.Profiles
	if view := rc.View(); view != nil {
		fields = append(fields, jsontext.M("view", jsontext.Strings(view)))
		declared = declared.With(kinds.LayerProfile) // a layered context is a context of the layer profile
	}
	desc, refusal := check.NormalIn(jsontext.O(fields...), declared)
	if refusal != nil {
		ops.Fail(refusal.Codes...)
	}
	ref := canon.Revision(desc)
	if gone := c.RemovedIn(covered...); len(gone) > 0 { // REL-RMV-10: no lock over a closure with a removed object
		reasons := make([]envelope.Reason, len(gone))
		for n, r := range gone {
			reasons[n] = envelope.R("removed", r)
		}
		fail("not_found", reasons)
	}
	members := ops.Sorted(c.Closure(covered...)) // the read comes before the write (STS-ENV-4)
	items := make([]envelope.Item, len(members))
	for n, r := range members {
		items[n] = envelope.Ref(r)
	}
	out := c
	if c.Store.Output != nil {
		out = c.On(c.Store.Output)
	}
	object := ops.Object{Ref: ref, Kind: check.Kind(desc), Text: canon.Canonical(desc), Targets: check.Targets(desc)}
	out.S.Update(func(tx ops.Tx) { tx.PutObject(object) })
	e := c.Envelope("ok", nil, items).WithContext(rc.ID)
	e.Fields = append(e.Fields, jsontext.M("lock", jsontext.S(ref)))
	return e
}

// ExecutionInputs is the operation `execution_inputs` (LCK-EXE-1 to LCK-EXE-4, LCK-CAP-1, LCK-CAP-2).
func ExecutionInputs(c *ops.Call) envelope.Envelope {
	c.Form("field_type", append([]ops.Field{ops.Req("context", ops.IsStr), ops.Req("binding_set", ops.IsRef),
		ops.Opt("context_inputs", ops.ListOf(ops.IsRef, true)), ops.Opt("requires", ops.IsNames),
		ops.Req("consumer", ops.IsText), ops.Req("completeness", ops.IsLabel)}, indexes(c)...)...)
	c.Counted("context_inputs", false) // LCK-EXE-1
	c.Counted("requires", false)       // LCK-CAP-1
	c.Counted("indexes", false)        // IDX-LCK-1
	rc := c.Context()
	bindingSet, _ := c.Request.Get("binding_set")
	refs := []target{{bindingSet.Str, "execution-lock", "binding_set"}}
	covered := []string{bindingSet.Str}
	inputs, _ := c.Request.Get("context_inputs")
	for _, r := range inputs.Arr {
		refs = append(refs, target{r.Str, "execution-lock", "context_inputs"})
		covered = append(covered, r.Str)
	}
	checkedRefs(c, rc, append(refs, indexTargets(c)...))
	required(c, rc)
	fresh(c, rc) // IDX-LCK-2: before LCK-EXE-3
	desc, _ := c.Descriptor(bindingSet.Str)
	bindingsProblems(c, desc)
	return lock(c, rc, covered)
}

// BuildInputs is the operation `build_inputs` (LCK-BLD-1 to LCK-BLD-3, LCK-CAP-1, LCK-CAP-2).
func BuildInputs(c *ops.Call) envelope.Envelope {
	c.Form("field_type", append([]ops.Field{ops.Req("context", ops.IsStr), ops.Req("inputs", ops.ListOf(ops.IsRef, true)),
		ops.Opt("requires", ops.IsNames), ops.Req("consumer", ops.IsText), ops.Req("completeness", ops.IsLabel)}, indexes(c)...)...)
	c.Counted("inputs", true) // LCK-BLD-1
	c.Counted("requires", false)
	c.Counted("indexes", false) // IDX-LCK-1
	rc := c.Context()
	inputs := ops.Strings(field(c.Request, "inputs"))
	refs := make([]target, len(inputs))
	for n, r := range inputs {
		refs[n] = target{r, "build-lock", "inputs"}
	}
	checkedRefs(c, rc, append(refs, indexTargets(c)...))
	required(c, rc)
	fresh(c, rc) // IDX-LCK-2
	return lock(c, rc, inputs)
}

func field(v jsontext.Value, name string) jsontext.Value {
	out, _ := v.Get(name)
	return out
}
