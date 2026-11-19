// The kind table of the base profile (document 02). The test of this package compares it with the tables
// of the document.
package kinds

// common has the fields of each kind (KND-SCH-1).
var common = []Field{
	{Name: "descriptor_profile", Type: "string", Count: "1"},
	{Name: "kind", Type: "string", Count: "1"},
	{Name: "logical_id", Type: "string", Count: "0..1"},
	{Name: "ext", Type: "json", Count: "0..1"},
}

// own has the fields of each kind, in the order of the document, with each group of the targets column expanded.
var own = map[string][]Field{
	"source": {
		{Name: "payload", Type: "payload", Count: "1"},
	},
	"reading": {
		{Name: "source", Type: "ref", Count: "1", Targets: []string{"source"}},
		{Name: "producer", Type: "string", Count: "1"},
		{Name: "producer_config", Type: "payload", Count: "0..1"},
		{Name: "coverage_profile", Type: "string", Count: "1"},
		{Name: "payload", Type: "payload", Count: "1"},
	},
	"excerpt": {
		{Name: "parent", Type: "ref", Count: "1", Targets: []string{"reading"}},
		{Name: "ranges", Type: "json", Count: "1"},
		{Name: "payload", Type: "payload", Count: "1"},
	},
	"schema": {
		{Name: "schema_profile", Type: "string", Count: "1"},
		{Name: "annotations", Type: "ref", Count: "0..1", Targets: []string{"artifact"}},
		{Name: "payload", Type: "payload", Count: "1"},
	},
	"data-resource": {
		{Name: "data_profile", Type: "string", Count: "1"},
		{Name: "schema", Type: "ref", Count: "1", Targets: []string{"schema"}},
		{Name: "payload", Type: "payload", Count: "1"},
	},
	"artifact": {
		{Name: "artifact_type", Type: "string", Count: "1"},
		{Name: "schema", Type: "ref", Count: "0..1", Targets: []string{"schema"}},
		{Name: "declared_roles", Type: "ref-map", Count: "0..n", Targets: []string{"schema"}},
		{Name: "runtime_profile", Type: "ref", Count: "0..1", Targets: []string{"runtime-profile"}},
		{Name: "payload", Type: "payload", Count: "1"},
	},
	"runtime-profile": {
		{Name: "profile_id", Type: "string", Count: "1"},
		{Name: "payload", Type: "payload", Count: "0..1"},
	},
	"binding-set": {
		{Name: "bindings", Type: "ref-map", Count: "0..n", Targets: []string{"artifact", "data-resource", "schema"}},
		{Name: "runtime_profiles", Type: "ref", Count: "0..n", Targets: []string{"runtime-profile"}},
		{Name: "parameters", Type: "json", Count: "0..1"},
	},
	"derivation": {
		{Name: "operands", Type: "ref", Count: "1..n list", Targets: []string{"data-resource", "excerpt", "reading"}},
		{Name: "operand_selectors", Type: "selector", Count: "0..n list"},
		{Name: "method", Type: "string", Count: "1"},
		{Name: "result", Type: "json", Count: "1"},
	},
	"evidence": {
		{Name: "subject", Type: "ref", Count: "1", Targets: []string{"artifact", "binding-set", "data-resource"}},
		{Name: "subject_selector", Type: "selector", Count: "0..1"},
		{Name: "supports", Type: "ref", Count: "1", Targets: []string{"derivation", "excerpt", "reading"}},
		{Name: "support_selector", Type: "selector", Count: "0..1"},
		{Name: "origin", Type: "string", Count: "1"},
	},
	"receipt": {
		{Name: "producer", Type: "string", Count: "1"},
		{Name: "subjects", Type: "ref", Count: "0..n", Targets: []string{"artifact", "binding-set", "data-resource", "excerpt", "knowledge-unit", "reading", "runtime-profile", "schema", "source"}},
		{Name: "payload", Type: "payload", Count: "1"},
	},
	"check": {
		{Name: "subject", Type: "ref", Count: "1", Targets: []string{"artifact", "binding-set", "data-resource", "evidence", "excerpt", "knowledge-unit", "reading", "relation-set", "runtime-profile", "schema", "source"}},
		{Name: "check_profile", Type: "string", Count: "1"},
		{Name: "outcome", Type: "string", Count: "1"},
		{Name: "scope", Type: "json", Count: "1"},
		{Name: "receipts", Type: "ref", Count: "0..n", Targets: []string{"receipt"}},
		{Name: "origin", Type: "string", Count: "1"},
	},
	"retraction": {
		{Name: "target", Type: "ref", Count: "1", Targets: []string{"check", "evidence"}},
		{Name: "reason", Type: "string", Count: "1"},
		{Name: "origin", Type: "string", Count: "1"},
	},
	"supersession": {
		{Name: "target", Type: "ref", Count: "1", Targets: []string{"check", "evidence"}},
		{Name: "replacement", Type: "ref", Count: "1", Targets: []string{"check", "evidence"}},
		{Name: "reason", Type: "string", Count: "1"},
		{Name: "origin", Type: "string", Count: "1"},
	},
	"relation-set": {
		{Name: "members", Type: "ref", Count: "0..n", Targets: []string{"check", "derivation", "evidence", "retraction", "supersession"}},
	},
	"overlay": {
		{Name: "subject", Type: "ref", Count: "1", Targets: []string{"artifact", "binding-set", "data-resource", "excerpt", "knowledge-unit", "reading", "runtime-profile", "schema", "source"}},
		{Name: "origin", Type: "string", Count: "1"},
		{Name: "precedence", Type: "string", Count: "0..1"},
		{Name: "values", Type: "json", Count: "1"},
	},
	"overlay-set": {
		{Name: "members", Type: "ref", Count: "0..n", Targets: []string{"overlay"}},
	},
	"knowledge-unit": {
		{Name: "binding_set", Type: "ref", Count: "1", Targets: []string{"binding-set"}},
		{Name: "narratives", Type: "ref", Count: "0..n", Targets: []string{"artifact"}},
		{Name: "relation_sets", Type: "ref", Count: "0..n", Targets: []string{"relation-set"}},
		{Name: "overlay_sets", Type: "ref", Count: "0..n", Targets: []string{"overlay-set"}},
	},
	"snapshot": {
		{Name: "collection", Type: "string", Count: "1"},
		{Name: "units", Type: "ref", Count: "0..n", Targets: []string{"knowledge-unit"}},
		{Name: "schemas", Type: "ref", Count: "0..n", Targets: []string{"schema"}},
		{Name: "relation_sets", Type: "ref", Count: "0..n", Targets: []string{"relation-set"}},
		{Name: "overlay_sets", Type: "ref", Count: "0..n", Targets: []string{"overlay-set"}},
		{Name: "observations", Type: "ref", Count: "0..n", Targets: []string{"observation"}},
		{Name: "native_mappings", Type: "ref", Count: "0..n", Targets: []string{"native-mapping"}},
	},
	"observation": {
		{Name: "subjects", Type: "ref", Count: "1..n", Targets: []string{"artifact", "binding-set", "check", "data-resource", "derivation", "evidence", "excerpt", "knowledge-unit", "observation", "overlay", "reading", "receipt", "retraction", "runtime-profile", "schema", "source", "supersession"}},
		{Name: "observation_type", Type: "string", Count: "1"},
		{Name: "scope", Type: "json", Count: "1"},
		{Name: "values", Type: "json", Count: "1"},
	},
	"native-mapping": {
		{Name: "subject", Type: "ref", Count: "1", Targets: []string{"artifact", "binding-set", "check", "data-resource", "derivation", "evidence", "excerpt", "knowledge-unit", "observation", "overlay", "reading", "receipt", "retraction", "runtime-profile", "schema", "source", "supersession"}},
		{Name: "native_scheme", Type: "string", Count: "1"},
		{Name: "native_id", Type: "string", Count: "1"},
		{Name: "checked", Type: "json", Count: "1"},
	},
	"execution-lock": {
		{Name: "binding_set", Type: "ref", Count: "1", Targets: []string{"binding-set"}},
		{Name: "context_inputs", Type: "ref", Count: "0..n", Targets: []string{"artifact", "binding-set", "check", "data-resource", "derivation", "evidence", "excerpt", "knowledge-unit", "observation", "overlay", "overlay-set", "reading", "receipt", "relation-set", "retraction", "runtime-profile", "schema", "source", "supersession"}},
		{Name: "requires", Type: "string", Count: "0..n"},
		{Name: "consumer", Type: "string", Count: "1"},
		{Name: "completeness", Type: "string", Count: "1"},
	},
	"build-lock": {
		{Name: "inputs", Type: "ref", Count: "1..n", Targets: []string{"artifact", "binding-set", "check", "data-resource", "derivation", "evidence", "excerpt", "knowledge-unit", "observation", "overlay", "overlay-set", "reading", "receipt", "relation-set", "retraction", "runtime-profile", "schema", "snapshot", "source", "supersession"}},
		{Name: "requires", Type: "string", Count: "0..n"},
		{Name: "consumer", Type: "string", Count: "1"},
		{Name: "completeness", Type: "string", Count: "1"},
	},
	"activation": {
		{Name: "candidate", Type: "string", Count: "1"},
		{Name: "base", Type: "ref", Count: "0..1", Targets: []string{"snapshot"}},
		{Name: "lock", Type: "ref", Count: "1", Targets: []string{"build-lock", "execution-lock"}},
		{Name: "approval", Type: "ref", Count: "1", Targets: []string{"receipt"}},
		{Name: "report", Type: "ref", Count: "1", Targets: []string{"receipt"}},
		{Name: "partition", Type: "string", Count: "1"},
		{Name: "effective_from", Type: "string", Count: "1"},
		{Name: "effective_until", Type: "string", Count: "0..1"},
		{Name: "request_key", Type: "string", Count: "1"},
		{Name: "producer", Type: "string", Count: "1"},
		{Name: "runner", Type: "string", Count: "1"},
		{Name: "approver", Type: "string", Count: "1"},
		{Name: "removed", Type: "ref", Count: "0..n", Targets: []string{"knowledge-unit", "native-mapping", "observation", "overlay-set", "relation-set", "schema"}},
	},
}
