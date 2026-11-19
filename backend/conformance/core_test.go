package conformance

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mohanraj00/aavanam/backend/core/memstore"
)

func newCore(t *testing.T) *Core {
	t.Helper()
	core := &Core{Open: func() (map[string]Backing, error) {
		device := memstore.New()
		return map[string]Backing{"writer": memstore.New(), "device": device, "output": device.Output()}, nil
	}}
	if err := core.Reset(); err != nil {
		t.Fatal(err)
	}
	return core
}

func result(t *testing.T, core *Core, store, operation, request string) string {
	t.Helper()
	out, err := core.Call(store, json.RawMessage(operation), json.RawMessage(request), nil)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// The core gets the request text with no decode, so the checks of the JSON form stay visible.
func TestCoreKeepsTheRequestText(t *testing.T) {
	core := newCore(t)
	empty, _ := core.Fingerprint("writer")
	source := `{\"descriptor_profile\":\"aavanam/base/v1\",\"kind\":\"source\",\"payload\":{\"digest\":\"sha256:ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad\",\"media_type\":\"text/plain\",\"size\":3}}`
	for request, want := range map[string]string{
		`{"descriptors":["{\"a\":1,\"a\":2}"]}`:                   `"duplicate_key"`,
		`{"descriptors":["[\"\ud800\"]"]}`:                        `"non_ijson_value"`,
		`{"descriptors":["{\"kind\":\"source\",\"x\":1.0e400}"]}`: `"non_ijson_value"`,
		`{"descriptors":["` + source + `"]}`:                      `"payload_missing"`,
		`{"descriptors":["` + source + `"],"descriptors":[]}`:     `"duplicate_key"`,
	} {
		if got := result(t, core, "writer", `"put"`, request); !strings.Contains(got, want) {
			t.Errorf("%s: %s has no %s", request, got, want)
		}
	}
	if after, _ := core.Fingerprint("writer"); after != empty {
		t.Error("a refused put changed the store")
	}
	ok := result(t, core, "writer", `"put"`, `{"descriptors":["`+source+`"],"payloads":{"sha256:ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad":"YWJj"}}`)
	if !strings.Contains(ok, `"status":"ok"`) || !strings.Contains(ok, `"operation":"put"`) {
		t.Errorf("a correct put: %s", ok)
	}
	if after, _ := core.Fingerprint("writer"); after == empty {
		t.Error("a put did not change the fingerprint")
	}
	if err := core.Reset(); err != nil {
		t.Fatal(err)
	}
	if after, _ := core.Fingerprint("writer"); after != empty {
		t.Error("a reset kept content")
	}
}

func TestCoreStoresAndDeclarations(t *testing.T) {
	core := newCore(t)
	if got := result(t, core, "device", `"put"`, `{}`); !strings.Contains(got, `"read_only_store"`) {
		t.Errorf("a put on the device store: %s", got)
	}
	if got := result(t, core, "output", `"producer_get"`, `{}`); !strings.Contains(got, `"producer_section_absent"`) {
		t.Errorf("a producer operation on the output store: %s", got)
	}
	if got := result(t, core, "writer", `7`, `{}`); !strings.Contains(got, `"operation":null`) {
		t.Errorf("an operation that is not a name: %s", got)
	}
	if core.stores["device"].Output != core.stores["output"] || core.stores["writer"].Output != nil {
		t.Error("the device store does not have its output store (BUN-IMP-7)")
	}
	if _, err := core.Call("other", json.RawMessage(`"get"`), json.RawMessage(`{}`), nil); err == nil {
		t.Error("a store name that the driver does not have gave no error")
	}
	if _, err := core.Fingerprint("other"); err == nil {
		t.Error("a fingerprint of an unknown store gave no error")
	}
	// The three calls above were calls, so a declaration is too late on those stores (PUB-GAT-1).
	if core.Gate(json.RawMessage(`"c"`)) == nil {
		t.Error("a gate after the first call was accepted")
	}
	core = newCore(t)
	steps := map[string]error{
		"a gate":                 core.Gate(json.RawMessage(`"c"`)),
		"a section":              core.Section(json.RawMessage(`"c"`), json.RawMessage(`"serving"`)),
		"a policy":               core.Accept(json.RawMessage(`"p"`)),
		"a second section, same": core.Section(json.RawMessage(`"c"`), json.RawMessage(`"serving"`)),
	}
	for name, err := range steps {
		if err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	refused := map[string]error{
		"a gate that is not a string":   core.Gate(json.RawMessage(`7`)),
		"a section that is not a name":  core.Section(json.RawMessage(`"c"`), json.RawMessage(`"public"`)),
		"a different section":           core.Section(json.RawMessage(`"c"`), json.RawMessage(`"evidence"`)),
		"a policy that is not a string": core.Accept(json.RawMessage(`null`)),
		"a collection that is not JSON": core.Section(json.RawMessage(`c`), json.RawMessage(`"serving"`)),
	}
	for name, err := range refused {
		if err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	for _, name := range []string{"writer", "device", "output"} {
		if _, ok := core.stores[name].Policy("p"); !ok {
			t.Errorf("the store %s does not have the policy", name)
		}
	}
}
