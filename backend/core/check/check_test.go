package check

import (
	"reflect"
	"strings"
	"testing"

	"github.com/mohanraj00/aavanam/backend/core/canon"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/kinds"
)

const (
	refA = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	refB = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	refC = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

func value(t *testing.T, text string) jsontext.Value {
	t.Helper()
	text = strings.NewReplacer("$A", refA, "$B", refB, "$C", refC, "$P", `"descriptor_profile":"aavanam/base/v1"`).Replace(text)
	v, err := jsontext.Parse([]byte(text), jsontext.Lenient)
	if err != nil {
		t.Fatalf("%s: %v", text, err)
	}
	return v
}

func normal(t *testing.T, text string) jsontext.Value {
	t.Helper()
	desc, refusal := Normal(value(t, text))
	if refusal != nil {
		t.Fatalf("%s: %v", text, refusal.Codes)
	}
	return desc
}

// Steps 2 and 3 of STS-MAL-3: `unknown_kind`, `unknown_field`, then `field_count`, then `field_type`, with
// one code for each affected value.
func TestNormalRefusals(t *testing.T) {
	cases := []struct {
		text  string
		code  string
		count int
	}{
		{`[]`, "field_type", 1},
		{`{$P}`, "field_count", 1},
		{`{$P,"kind":7}`, "field_type", 1},
		{`{$P,"kind":"workspace"}`, "unknown_kind", 1},
		{`{$P,"kind":"snapshot","collection":"c","x":1,"y":2}`, "unknown_field", 2},
		{`{"kind":"snapshot"}`, "field_count", 2},                                                                 // descriptor_profile and collection
		{`{$P,"kind":"snapshot","collection":7}`, "field_type", 1},                                                // KND-SCH-4
		{`{$P,"kind":"snapshot","collection":"c","units":"$A"}`, "field_type", 1},                                 // a set is a list
		{`{$P,"kind":"snapshot","collection":"c","units":["$A","$A"]}`, "field_count", 1},                         // KND-SCH-5
		{`{$P,"kind":"snapshot","collection":"c","units":["$A","x",7]}`, "field_type", 2},                         // one for each value
		{`{$P,"kind":"snapshot","collection":"c","units":[7,7]}`, "field_count", 1},                               // the count comes first
		{`{$P,"kind":"observation","subjects":[],"observation_type":"t","scope":1,"values":1}`, "field_count", 1}, // 1..n
		{`{$P,"kind":"snapshot","collection":"c","logical_id":""}`, "field_type", 1},                              // IDN-LOG-1
		{`{$P,"kind":"snapshot","collection":"c\ud800"}`, "field_type", 1},
		{`{$P,"kind":"snapshot","collection":"c","ext":{"n":9007199254740993}}`, "field_type", 1},
		{`{$P,"kind":"binding-set","bindings":[]}`, "field_type", 1}, // KND-SCH-6
		{`{$P,"kind":"binding-set","bindings":{"r":"x"}}`, "field_type", 1},
		{`{$P,"kind":"source","payload":{"digest":"$A","media_type":"m","size":-1}}`, "field_type", 1},
		{`{$P,"kind":"source","payload":{"digest":"$A","media_type":"m","size":1,"x":0}}`, "field_type", 1},
		{`{$P,"kind":"source","payload":{"digest":"$A","media_type":"m","size":1.5}}`, "field_type", 1},
		{`{$P,"kind":"excerpt","parent":"$A","ranges":[],"payload":{"digest":"$B","media_type":"m","size":0}}`, "field_type", 1},
		{`{$P,"kind":"excerpt","parent":"$A","ranges":[{"start":2,"end":4},{"start":3,"end":5}],"payload":{"digest":"$B","media_type":"m","size":4}}`, "field_type", 1},
		{`{$P,"kind":"derivation","operands":["$A","$B"],"operand_selectors":[null],"method":"m","result":1}`, "field_count", 1}, // KND-SCH-7
		{`{$P,"kind":"derivation","operands":["$A"],"operand_selectors":[{"selector_profile":"aavanam/bytes/v1","start":3,"end":2}],"method":"m","result":1}`, "field_type", 1},
		{`{$P,"kind":"execution-lock","binding_set":"$A","consumer":"c","completeness":"Not A Label"}`, "field_type", 1}, // LCK-CMPL-1
	}
	for _, c := range cases {
		_, refusal := Normal(value(t, c.text))
		if refusal == nil || refusal.Codes[0] != c.code || len(refusal.Codes) != c.count {
			t.Errorf("%s: %v, not %s x %d", c.text, refusal, c.code, c.count)
		}
	}
}

// The normal form has no empty list of a field that can be absent, and each set in ascending order. Thus two
// texts of one descriptor have one revision (KND-SCH-3, KND-SCH-5, IDN-CAN-1).
func TestNormalForm(t *testing.T) {
	a := normal(t, `{"units":["$B","$A"],"schemas":[],"collection":"c","kind":"snapshot",$P}`)
	b := normal(t, `{$P,"kind":"snapshot","collection":"c","units":["$A","$B"]}`)
	if canon.Revision(a) != canon.Revision(b) {
		t.Errorf("two texts of one snapshot have two revisions:\n%s\n%s", canon.Canonical(a), canon.Canonical(b))
	}
	if got := strings.Join(a.Keys(), ","); got != "descriptor_profile,kind,collection,units" {
		t.Errorf("the fields: %s", got)
	}
	// A list keeps its order, and an empty ref-map stays.
	d := normal(t, `{$P,"kind":"derivation","operands":["$B","$A"],"operand_selectors":[null,{"selector_profile":"x/y/v1"}],"method":"m","result":{"z":1,"a":[]}}`)
	if refs := Targets(d); !reflect.DeepEqual(refs, []string{refA, refB}) {
		t.Errorf("targets: %v", refs)
	}
	if got := References(d); got[0] != (Reference{"operands", refB}) || got[1] != (Reference{"operands", refA}) {
		t.Errorf("a list changed its order: %v", got)
	}
	m := normal(t, `{$P,"kind":"binding-set","bindings":{}}`)
	if !m.Has("bindings") {
		t.Error("an empty ref-map is not in the normal form")
	}
	bs := normal(t, `{$P,"kind":"binding-set","bindings":{"b":"$B","a":"$A","c":"$A"},"runtime_profiles":["$C"]}`)
	if got := References(bs); len(got) != 4 || !reflect.DeepEqual(Targets(bs), []string{refA, refB, refC}) {
		t.Errorf("references of a binding set: %v", got)
	}
	src := normal(t, `{$P,"kind":"source","payload":{"digest":"$A","media_type":"text/plain","size":3}}`)
	if got := Payloads(src); len(got) != 1 || got[0] != (Payload{refA, "text/plain", 3}) || Kind(src) != "source" {
		t.Errorf("payloads: %v", got)
	}
}

type world struct {
	objects map[string]jsontext.Value
	data    map[string][]byte
}

func (w world) env() Env {
	return Env{
		Lookup: func(ref string) (jsontext.Value, bool) { v, ok := w.objects[ref]; return v, ok },
		Data:   func(digest string) ([]byte, bool) { b, ok := w.data[digest]; return b, ok },
	}
}

func codes(r *Refusal) string {
	if r == nil {
		return ""
	}
	return strings.Join(r.Codes, ",") + "|" + strings.Join(r.Refs, ",")
}

// Step 4 of STS-MAL-3.
func TestReferences(t *testing.T) {
	text := []byte("the whole reading")
	digest := jsontext.Digest(text)
	w := world{objects: map[string]jsontext.Value{}, data: map[string][]byte{digest: text}}
	source := normal(t, `{$P,"kind":"source","payload":{"digest":"`+digest+`","media_type":"text/plain","size":17}}`)
	w.objects[refA] = source
	reading := `{$P,"kind":"reading","source":"$A","producer":"p","coverage_profile":"aavanam/coverage/v1","payload":{"digest":"` + digest + `","media_type":"text/plain","size":17}}`
	if got := codes(w.env().References(normal(t, reading))); got != "" {
		t.Errorf("a reading of a source: %s", got)
	}
	missing := normal(t, strings.Replace(reading, "$A", "$B", 1))
	if got := codes(w.env().References(missing)); got != "missing_target|"+refB {
		t.Errorf("an absent target: %s", got)
	}
	incomplete := w.env()
	incomplete.Incomplete = true
	if got := codes(incomplete.References(missing)); got != "bundle_incomplete|"+refB {
		t.Errorf("an absent target of a bundle: %s", got)
	}
	excerpt := normal(t, `{$P,"kind":"excerpt","parent":"$A","ranges":[{"start":0,"end":3}],"payload":{"digest":"$C","media_type":"m","size":3}}`)
	if got := codes(w.env().References(excerpt)); got != "unsupported_target_kind|"+refA {
		t.Errorf("a parent that is a source: %s", got)
	}
}

// Steps 5 and 6 of STS-MAL-3.
func TestPayloadsAndContent(t *testing.T) {
	whole, part := []byte("the whole reading"), []byte("thereading")
	wd, pd := jsontext.Digest(whole), jsontext.Digest(part)
	reading := normal(t, `{$P,"kind":"reading","source":"$A","producer":"p","coverage_profile":"c/c/v1","payload":{"digest":"`+wd+`","media_type":"text/plain","size":17}}`)
	excerpt := normal(t, `{$P,"kind":"excerpt","parent":"$B","ranges":[{"start":0,"end":3},{"start":10,"end":17}],"payload":{"digest":"`+pd+`","media_type":"text/plain","size":10}}`)
	w := world{objects: map[string]jsontext.Value{refB: reading}, data: map[string][]byte{wd: whole, pd: part}}
	if got := codes(w.env().Payloads(excerpt)) + codes(w.env().Content(excerpt)); got != "" {
		t.Errorf("a correct excerpt: %s", got)
	}
	w.data[pd] = []byte("thereadinG")
	if got := codes(w.env().Payloads(excerpt)); got != "digest_mismatch|" {
		t.Errorf("changed bytes: %s", got)
	}
	short := normal(t, `{$P,"kind":"source","payload":{"digest":"`+wd+`","media_type":"m","size":16}}`)
	if got := codes(w.env().Payloads(short)); got != "size_mismatch|" {
		t.Errorf("an incorrect size: %s", got)
	}
	delete(w.data, pd)
	if got := codes(w.env().Payloads(excerpt)); got != "payload_missing|" {
		t.Errorf("absent bytes: %s", got)
	}
	withheld := w.env()
	withheld.Incomplete, withheld.Withheld = true, map[string]bool{pd: true}
	if got := codes(withheld.Payloads(excerpt)) + codes(withheld.Content(excerpt)); got != "" {
		t.Errorf("withheld bytes defer the checks (BUN-IMP-9): %s", got)
	}
	// The ranges select other bytes than the payload (BUN-EXC-3).
	other := []byte("the reading")
	od := jsontext.Digest(other)
	w.data[od] = other
	wrong := normal(t, `{$P,"kind":"excerpt","parent":"$B","ranges":[{"start":0,"end":3}],"payload":{"digest":"`+od+`","media_type":"m","size":11}}`)
	if got := codes(w.env().Content(wrong)); got != "excerpt_mismatch|" {
		t.Errorf("an excerpt with other bytes: %s", got)
	}
	outside := normal(t, `{$P,"kind":"excerpt","parent":"$B","ranges":[{"start":0,"end":99}],"payload":{"digest":"`+od+`","media_type":"m","size":11}}`)
	if got := codes(w.env().Content(outside)); got != "excerpt_mismatch|" {
		t.Errorf("a range after the end: %s", got)
	}
}

// IDN-NAT-3 and PUB-SEC-3.
func TestSnapshotContent(t *testing.T) {
	mapping := func(subject, id string) jsontext.Value {
		return normal(t, `{$P,"kind":"native-mapping","subject":"`+subject+`","native_scheme":"s","native_id":"`+id+`","checked":true}`)
	}
	w := world{objects: map[string]jsontext.Value{
		"m1": mapping(refA, "1"), "m2": mapping(refB, "1"), "m3": mapping(refA, "2"), "m4": mapping(refA, "1"),
	}}
	snapshot := func(mappings string) jsontext.Value {
		return jsontext.O(jsontext.M("kind", jsontext.S("snapshot")), jsontext.M("native_mappings", jsontext.Strings(strings.Split(mappings, ","))))
	}
	if got := codes(w.env().Content(snapshot("m1,m3,m4"))); got != "" {
		t.Errorf("one subject for each native identity: %s", got)
	}
	if got := codes(w.env().Content(snapshot("m1,m2,m3"))); got != "native_id_conflict|" {
		t.Errorf("two subjects for one native identity: %s", got)
	}
	source := normal(t, `{$P,"kind":"source","payload":{"digest":"$A","media_type":"m","size":1}}`)
	reading := normal(t, `{$P,"kind":"reading","source":"$A","producer":"p","coverage_profile":"c/c/v1","payload":{"digest":"$A","media_type":"m","size":1}}`)
	set := normal(t, `{$P,"kind":"binding-set"}`)
	unit := normal(t, `{$P,"kind":"knowledge-unit","binding_set":"$C"}`)
	w = world{objects: map[string]jsontext.Value{refA: source, refB: reading, refC: set, "unit": unit}}
	serving := jsontext.O(jsontext.M("kind", jsontext.S("snapshot")), jsontext.M("units", jsontext.Strings([]string{"unit"})),
		jsontext.M("observations", jsontext.Strings([]string{refB, "absent"})))
	if got := w.env().SectionViolations(serving); !reflect.DeepEqual(got, []string{refA, refB}) {
		t.Errorf("the Evidence material in the closure: %v", got)
	}
}

func TestSelectorForm(t *testing.T) {
	for text, want := range map[string]bool{
		`{"selector_profile":"aavanam/bytes/v1","start":0,"end":0}`:                true,
		`{"selector_profile":"aavanam/nfc-text/v1","start":1,"end":2}`:             true,
		`{"selector_profile":"aavanam/bytes/v1","start":2,"end":1}`:                false,
		`{"selector_profile":"aavanam/bytes/v1","start":0}`:                        false,
		`{"selector_profile":"aavanam/bytes/v1","start":0,"end":1,"x":1}`:          false,
		`{"selector_profile":"aavanam/bytes/v1","start":"0","end":1}`:              false,
		`{"selector_profile":"other/profile/v1","anything":[1]}`:                   true, // REL-SEL-1: the operation refuses it
		`{"selector_profile":7}`:                                                   false,
		`"aavanam/bytes/v1"`:                                                       false,
		`{"selector_profile":"aavanam/bytes/v1","start":-1,"end":1}`:               false,
		`{"selector_profile":"aavanam/bytes/v1","start":0,"end":1.5}`:              false,
		`{"selector_profile":"aavanam/bytes/v1","start":0,"end":9007199254740993}`: false,
	} {
		if got := IsSelector(value(t, text)); got != want {
			t.Errorf("IsSelector(%s) = %v", text, got)
		}
	}
	if !IsLabel("aavanam/declared-refs/v1") || IsLabel("aavanam/declared-refs/v0") || IsLabel("a/b/v1\n") || IsLabel("A/b/v1") {
		t.Error("IsLabel")
	}
}

// LYR-PRF-1 and IDX-PRF-1: a kind or a field of a profile is registered only for a store that declares the
// profile. IDX-MTH-1: the parameters of the method `aavanam/hashed-terms/v1`.
func TestNormalWithProfiles(t *testing.T) {
	layers := kinds.Profiles{kinds.LayerProfile: true}
	indexes := kinds.Profiles{kinds.IndexProfile: true}
	const (
		layer = `{$P,"kind":"layer","base":"c","scope":"s"}`
		lock  = `{$P,"kind":"build-lock","inputs":["$A"],"consumer":"c","completeness":"a/b/v1"`
		index = `{$P,"kind":"retrieval-index","snapshot":"$A","method":"aavanam/hashed-terms/v1","parameters":`
	)
	cases := []struct {
		text     string
		declared kinds.Profiles
		code     string
		count    int
	}{
		{layer, nil, "unknown_kind", 1},
		{layer, indexes, "unknown_kind", 1},
		{layer, layers, "", 0},
		{`{$P,"kind":"layer","base":"c"}`, layers, "field_count", 1},
		{`{$P,"kind":"layer","base":"c","scope":"s","collection":"c"}`, layers, "unknown_field", 1},
		{`{$P,"kind":"layer","base":"c","scope":"s","units":["$A","$A"]}`, layers, "field_count", 1},
		{lock + `,"view":["$A"]}`, nil, "unknown_field", 1},
		{lock + `,"view":["$A"]}`, indexes, "unknown_field", 1},
		{lock + `,"view":["$B","$A"]}`, layers, "", 0},
		{lock + `,"indexes":["$A"]}`, layers, "unknown_field", 1},
		{lock + `,"indexes":["$A"],"view":["$A"]}`, nil, "unknown_field", 2},
		{lock + `,"indexes":["$A"]}`, indexes, "", 0},
		{index + `{"dimensions":64}}`, nil, "unknown_kind", 1},
		{index + `{"dimensions":64}}`, layers, "unknown_kind", 1},
		{index + `{"dimensions":1}}`, indexes, "", 0},
		{index + `{"dimensions":65536}}`, indexes, "", 0},
		{index + `{"dimensions":0}}`, indexes, "field_type", 1},
		{index + `{"dimensions":65537}}`, indexes, "field_type", 1},
		{index + `{"dimensions":1.5}}`, indexes, "field_type", 1},
		{index + `{"dimensions":"64"}}`, indexes, "field_type", 1},
		{index + `{"dimensions":64,"seed":1}}`, indexes, "field_type", 1},
		{index + `{}}`, indexes, "field_type", 1},
		{index + `64}`, indexes, "field_type", 1},
		{index + `{"dimensions":9007199254740993}}`, indexes, "field_type", 1}, // one reason for a value that is not I-JSON
		{`{$P,"kind":"retrieval-index","snapshot":"$A","method":"aavanam/hashed-terms/v1"}`, indexes, "field_count", 1},
		// IDX-MTH-2: the store keeps the parameters of each other method as an opaque value.
		{`{$P,"kind":"retrieval-index","snapshot":"$A","method":"sample/embedding/v2","parameters":{"model":"m","seed":1}}`, indexes, "", 0},
		{`{$P,"kind":"retrieval-index","snapshot":"$A","method":"sample/embedding/v2","parameters":null}`, indexes, "", 0},
	}
	for _, c := range cases {
		desc, refusal := NormalIn(value(t, c.text), c.declared)
		switch {
		case c.code == "" && refusal != nil:
			t.Errorf("%s with %v: %v", c.text, c.declared.Names(), refusal.Codes)
		case c.code != "" && (refusal == nil || refusal.Codes[0] != c.code || len(refusal.Codes) != c.count):
			t.Errorf("%s with %v: %v, not %s x %d", c.text, c.declared.Names(), refusal, c.code, c.count)
		case c.code == "" && Kind(desc) == "build-lock":
			if view, has := desc.Get("view"); has && (len(view.Arr) != 2 || view.Arr[0].Str > view.Arr[1].Str) {
				t.Errorf("the view of a lock is not a set in ascending order: %v", view.Arr) // KND-SCH-5
			}
		}
	}
	// The references of a stored descriptor of a profile are dependencies with no declaration (KND-DEP-1).
	stored, _ := NormalIn(value(t, lock+`,"view":["$B"],"indexes":["$C"]}`), layers.With(kinds.IndexProfile))
	if got := Targets(stored); !reflect.DeepEqual(got, []string{refA, refB, refC}) {
		t.Errorf("the targets of a lock with a view and indexes: %v", got)
	}
}

// LYR-DEF-4: one native identity of a layer has one subject, as in a snapshot (IDN-NAT-3).
func TestLayerNativeIdentities(t *testing.T) {
	mapping := func(subject string) jsontext.Value {
		return normal(t, `{$P,"kind":"native-mapping","subject":"`+subject+`","native_scheme":"s","native_id":"1","checked":true}`)
	}
	objects := map[string]jsontext.Value{refA: mapping(refC), refB: mapping(refB)}
	env := Env{Lookup: func(ref string) (jsontext.Value, bool) { desc, ok := objects[ref]; return desc, ok }}
	layer, refusal := NormalIn(value(t, `{$P,"kind":"layer","base":"c","scope":"s","native_mappings":["$A","$B"]}`), kinds.Profiles{kinds.LayerProfile: true})
	if refusal != nil {
		t.Fatal(refusal.Codes)
	}
	if got := env.Content(layer); got == nil || !reflect.DeepEqual(got.Codes, []string{"native_id_conflict"}) {
		t.Errorf("a layer with two subjects of one native identity: %v", got)
	}
	if collection, ok := CollectionOf(layer); !ok || collection != "c" {
		t.Errorf("the base collection of a layer: %q %v", collection, ok)
	}
	if _, ok := CollectionOf(objects[refA]); ok {
		t.Error("a native mapping has a collection")
	}
}
