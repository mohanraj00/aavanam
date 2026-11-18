// Command entelos_records makes the activation records, signed records,
// key sets and content manifests for the activation seam test and the library
// example using the entelos Host.
package main

import (
	"bytes"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	lifecycle "github.com/mohanraj00/entelos/src/lifecycle-core"
)

type fixedClock string

func (c fixedClock) Now() (string, error) { return string(c), nil }

type mockRuntime struct{}

func (m *mockRuntime) Build() (string, error) { return "mock-build", nil }
func (m *mockRuntime) Execute(files map[string][]byte, command string, inputs []byte, cases any) (any, error) {
	return nil, nil
}

type testRunner struct{}

func (r *testRunner) Identity() (string, error) { return "sha256:" + strings.Repeat("0", 64), nil }
func (r *testRunner) Expected(lifecycle.Candidate) string { return "sha256:" + strings.Repeat("0", 64) }
func (r *testRunner) Qualify(in lifecycle.QualificationInput) (lifecycle.Verdict, error) {
	return lifecycle.Verdict{Outcome: lifecycle.Object{
		"status":             "passed",
		"bundle_ref":         in.Candidate.BundleRef,
		"implementation_ref": "sha256:" + strings.Repeat("0", 64),
	}}, nil
}

type collectPublisher struct {
	records map[string][]byte
	signed  map[string][]byte
	current string
}

func newCollectPublisher() *collectPublisher {
	return &collectPublisher{
		records: make(map[string][]byte),
		signed:  make(map[string][]byte),
	}
}

func (p *collectPublisher) Publish(record, signed []byte) error {
	p.records[p.current] = record
	p.signed[p.current] = signed
	return nil
}

func loadSeed(path string) []byte {
	data, err := os.ReadFile(path)
	if err != nil {
		// Public test seed: 32 bytes of 0x42. Never use outside tests.
		return bytes.Repeat([]byte{0x42}, 32)
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		decoded, err := hex.DecodeString(line)
		if err == nil && len(decoded) == 32 {
			return decoded
		}
	}
	return bytes.Repeat([]byte{0x42}, 32)
}

func writeFile(path string, content []byte) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		panic(err)
	}
	if err := os.WriteFile(path, content, 0644); err != nil {
		panic(err)
	}
}

func main() {
	var outDir, exampleDir string
	flag.StringVar(&outDir, "out", "", "output directory for seam records")
	flag.StringVar(&exampleDir, "example", "", "output directory for library example records")
	flag.Parse()

	if outDir == "" {
		outDir = "."
	}
	if exampleDir == "" {
		exampleDir = filepath.Join(outDir, "../../examples/library_hours")
	}

	seedPath := filepath.Join(outDir, "seed.txt")
	seed := loadSeed(seedPath)

	keyDoc, err := lifecycle.NewSigningKey(bytes.NewReader(seed))
	if err != nil {
		panic(err)
	}
	signer, err := lifecycle.NewSigner(keyDoc)
	if err != nil {
		panic(err)
	}

	keySetDoc := lifecycle.Object{
		"format": "entelos.keyset/1",
		"signer": "sample-signer",
		"keys":   lifecycle.Array{signer.PublicKey()},
	}
	keySetBytes := lifecycle.Encode(keySetDoc)
	keys, err := lifecycle.ParseKeySet(keySetBytes)
	if err != nil {
		panic(err)
	}

	store := lifecycle.NewMemoryStore()
	publisher := newCollectPublisher()
	runner := &testRunner{}
	runtime := &mockRuntime{}

	noticeSubject := "content:sample-notices/sample-notices"
	librarySubject := "content:sample-library-hours/sample-library-hours"

	ctx := lifecycle.Object{
		"scope":        "sample-scope",
		"runner_actor": "sample-runner",
		"worker":       "sample-worker",
		"functions":    lifecycle.Array{noticeSubject, librarySubject},
	}
	host, err := lifecycle.NewHostWithRunner(store, runtime, runner, ctx)
	if err != nil {
		panic(err)
	}
	host.Clock = fixedClock("2026-01-01T00:00:00Z")
	host.SetSigner(signer)
	host.SetPublisher(publisher)

	var reqCount int
	write := func(actor, role, op string, fields lifecycle.Object) lifecycle.Object {
		root, err := store.Read(nil)
		if err != nil {
			panic(err)
		}
		reqCount++
		q := lifecycle.Object{
			"op":                op,
			"expected_revision": root.Cursor,
			"request_key":       fmt.Sprintf("%s-%d", op, reqCount),
		}
		for k, v := range fields {
			q[k] = v
		}
		out, err := host.HandleAs(actor, []string{role}, q)
		if err != nil {
			panic(fmt.Sprintf("%s: %v", op, err))
		}
		return out.(lifecycle.Object)["result"].(lifecycle.Object)
	}

	// 1. Notice collection setup
	sourceNotice := write("sample-approver", "reviewer", "source", lifecycle.Object{
		"function":   noticeSubject,
		"from":       "2026-01-01",
		"until":      "2027-01-01",
		"text":       "sample notice rules",
		"reason":     "initial source",
		"supersedes": lifecycle.Array{},
	})
	planNotice := write("sample-approver", "reviewer", "plan", lifecycle.Object{
		"source_ref": sourceNotice["source_ref"],
		"cases":      lifecycle.Array{lifecycle.Object{"name": "case-notice"}},
		"origin":     "independent",
		"supersedes": lifecycle.Array{},
	})

	manifestNotice1 := `{"format":"entelos.content/1","collection":"sample-notices","partition":"sample-notices","base":"","items":[{"item_id":"inputs.lock","digest":"sha256:17cd2cd7b741ca624117d38b943f45f4bf9d306d3787f62794dc2e3acaf978e0","size":210},{"item_id":"member.5636ef0c5d0eb9d12482076a5a26efa2e34c42804da2ee8c1149ca633b0fed9a","digest":"sha256:5636ef0c5d0eb9d12482076a5a26efa2e34c42804da2ee8c1149ca633b0fed9a","size":252}]}`
	manifestNotice2 := `{"format":"entelos.content/1","collection":"sample-notices","partition":"sample-notices","base":"sha256:cb143e36ba5c1e4810d2ecbe5920e11610b64391deba4655aae1ffc60fadc0e8","items":[{"item_id":"inputs.lock","digest":"sha256:b709e36c41c0af5f1b9e712390e4b6de15bc8bc55bdd6357298cf2518ec02f72","size":210},{"item_id":"member.42b7c04a0878fe281292521a1fd3f3ecdc53a94d165940aa40fc40a387eb4ae1","digest":"sha256:42b7c04a0878fe281292521a1fd3f3ecdc53a94d165940aa40fc40a387eb4ae1","size":252}]}`

	// Notice update 1
	propNotice1 := write("sample-producer", "author", "propose", lifecycle.Object{
		"candidate":  lifecycle.Object{"kind": "content", "manifest": manifestNotice1},
		"from":       "2026-01-01",
		"until":      "2026-07-01",
		"source_ref": sourceNotice["source_ref"],
		"reason":     "first notice update",
	})
	qualNotice1 := write("sample-runner", "runner", "qualify", lifecycle.Object{
		"snapshot_ref": propNotice1["snapshot_ref"],
		"plan_ref":     planNotice["plan_ref"],
	})
	decNotice1 := write("sample-approver", "reviewer", "decide", lifecycle.Object{
		"report_ref": qualNotice1["report_ref"],
		"reason":     "approved notice update 1",
	})
	publisher.current = "notice1"
	actNotice1 := write("sample-activator", "reviewer", "activate", lifecycle.Object{
		"decision_ref": decNotice1["decision_ref"],
		"targets":      lifecycle.Array{},
		"request_key":  "sample-request-0001",
	})

	// Notice update 2
	sourceNotice2 := write("sample-approver", "reviewer", "source", lifecycle.Object{
		"function":   noticeSubject,
		"from":       "2026-07-01",
		"until":      "2027-01-01",
		"text":       "sample notice rules, second update",
		"reason":     "second source",
		"supersedes": lifecycle.Array{sourceNotice["source_ref"]},
	})
	planNotice2 := write("sample-approver", "reviewer", "plan", lifecycle.Object{
		"source_ref": sourceNotice2["source_ref"],
		"cases":      lifecycle.Array{lifecycle.Object{"name": "case-notice-2"}},
		"origin":     "independent",
		"supersedes": lifecycle.Array{},
	})
	propNotice2 := write("sample-producer", "author", "propose", lifecycle.Object{
		"candidate":  lifecycle.Object{"kind": "content", "manifest": manifestNotice2},
		"from":       "2026-07-01",
		"until":      "2027-01-01",
		"source_ref": sourceNotice2["source_ref"],
		"reason":     "second notice update",
	})
	qualNotice2 := write("sample-runner", "runner", "qualify", lifecycle.Object{
		"snapshot_ref": propNotice2["snapshot_ref"],
		"plan_ref":     planNotice2["plan_ref"],
	})
	decNotice2 := write("sample-approver", "reviewer", "decide", lifecycle.Object{
		"report_ref": qualNotice2["report_ref"],
		"reason":     "approved notice update 2",
	})
	publisher.current = "notice2"
	write("sample-activator", "reviewer", "activate", lifecycle.Object{
		"decision_ref": decNotice2["decision_ref"],
		"targets":      lifecycle.Array{},
		"request_key":  "sample-request-0002",
	})

	// 2. Library collection setup
	sourceLib := write("sample-approver", "reviewer", "source", lifecycle.Object{
		"function":   librarySubject,
		"from":       "2026-01-01",
		"until":      "2027-01-01",
		"text":       "sample library rules",
		"reason":     "initial source",
		"supersedes": lifecycle.Array{},
	})
	planLib := write("sample-approver", "reviewer", "plan", lifecycle.Object{
		"source_ref": sourceLib["source_ref"],
		"cases":      lifecycle.Array{lifecycle.Object{"name": "case-library"}},
		"origin":     "independent",
		"supersedes": lifecycle.Array{},
	})

	manifestLibrary := `{"format":"entelos.content/1","collection":"sample-library-hours","partition":"sample-library-hours","base":"","items":[{"item_id":"inputs.lock","digest":"sha256:fec3ea03cf16d8887fedd79b85630919cab3e640d79735bb1ec4802afade6dbd","size":366},{"item_id":"member.25b8af3b2a4093192d997df02765e012910038ffc626b027082a14d704ac0224","digest":"sha256:25b8af3b2a4093192d997df02765e012910038ffc626b027082a14d704ac0224","size":221},{"item_id":"member.805d17bb090ace84f92e4136a148f4aa718eb25c28de120bf11e0d1c53dd4674","digest":"sha256:805d17bb090ace84f92e4136a148f4aa718eb25c28de120bf11e0d1c53dd4674","size":152},{"item_id":"member.b5ed1806bfd075b2aab34380c9b6c9716ec47838f112d81b224797bd527a9d24","digest":"sha256:b5ed1806bfd075b2aab34380c9b6c9716ec47838f112d81b224797bd527a9d24","size":221}]}`

	propLib := write("sample-producer", "author", "propose", lifecycle.Object{
		"candidate":  lifecycle.Object{"kind": "content", "manifest": manifestLibrary},
		"from":       "2026-01-01",
		"until":      "2027-01-01",
		"source_ref": sourceLib["source_ref"],
		"reason":     "first library update",
	})
	qualLib := write("sample-runner", "runner", "qualify", lifecycle.Object{
		"snapshot_ref": propLib["snapshot_ref"],
		"plan_ref":     planLib["plan_ref"],
	})
	decLib := write("sample-approver", "reviewer", "decide", lifecycle.Object{
		"report_ref": qualLib["report_ref"],
		"reason":     "approved library update",
	})
	publisher.current = "library1"
	write("sample-activator", "reviewer", "activate", lifecycle.Object{
		"decision_ref": decLib["decision_ref"],
		"targets":      lifecycle.Array{},
		"request_key":  "library-request-0001",
	})

	// Retrieve signed event record for refusal vector test
	root, err := store.Read(nil)
	if err != nil {
		panic(err)
	}
	sigMap := root.State["signatures"].(lifecycle.Object)
	eventRef := actNotice1["event_ref"].(string)
	signedEventRef := sigMap[eventRef].(string)
	eventRecordOut, err := host.HandleAs("sample-approver", []string{"reviewer"}, lifecycle.Object{
		"op":   "record",
		"ref":  signedEventRef,
		"kind": "signed",
	})
	if err != nil {
		panic(err)
	}
	eventSignedBytes := lifecycle.Encode(eventRecordOut)

	// Verify all records
	for _, name := range []string{"notice1", "notice2", "library1"} {
		signed := publisher.signed[name]
		v, err := lifecycle.VerifyActivation(signed, keys)
		if err != nil {
			panic(fmt.Sprintf("%s verification failed: %v", name, err))
		}
		r := v["record"].(lifecycle.Object)
		fmt.Printf("verified %s: cand=%s base=%s req=%s\n", name,
			r["candidate"].(lifecycle.Object)["ref"], r["base"], r["request_key"])
	}

	evVer, err := lifecycle.VerifySigned(eventSignedBytes, keys)
	if err != nil || evVer["record_kind"] != "event" {
		panic(fmt.Sprintf("event signed verification failed: %v", err))
	}
	fmt.Printf("verified event: ref=%s\n", evVer["record_ref"])

	// Write seam files under outDir
	writeFile(filepath.Join(outDir, "keyset.json"), append(keySetBytes, '\n'))
	writeFile(filepath.Join(outDir, "manifest_first.json"), append([]byte(manifestNotice1), '\n'))
	writeFile(filepath.Join(outDir, "manifest_second.json"), append([]byte(manifestNotice2), '\n'))
	writeFile(filepath.Join(outDir, "first_record.json"), append(publisher.records["notice1"], '\n'))
	writeFile(filepath.Join(outDir, "first_signed.json"), append(publisher.signed["notice1"], '\n'))
	writeFile(filepath.Join(outDir, "second_record.json"), append(publisher.records["notice2"], '\n'))
	writeFile(filepath.Join(outDir, "second_signed.json"), append(publisher.signed["notice2"], '\n'))
	writeFile(filepath.Join(outDir, "event_signed.json"), append(eventSignedBytes, '\n'))

	// Write example files under exampleDir
	writeFile(filepath.Join(exampleDir, "keyset.json"), append(keySetBytes, '\n'))
	writeFile(filepath.Join(exampleDir, "manifest.json"), append([]byte(manifestLibrary), '\n'))
	writeFile(filepath.Join(exampleDir, "first_record.json"), append(publisher.records["library1"], '\n'))
	writeFile(filepath.Join(exampleDir, "first_signed.json"), append(publisher.signed["library1"], '\n'))

	fmt.Println("wrote all record files successfully")
}
