package devicetest

import (
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mohanraj00/aavanam/backend/conformance"
	"github.com/mohanraj00/aavanam/backend/core/ops"
	"github.com/mohanraj00/aavanam/reference/device"
)

// release060 is the release store that release 0.6.0 wrote (fixtures/README.md, "Release stores").
var release060 = filepath.Join("..", "..", "fixtures", "releases", "0.6.0")

// releaseFile reads one JSON file of the release store.
func releaseFile(t *testing.T, value any, path ...string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(append([]string{release060}, path...)...))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, value); err != nil {
		t.Fatal(err)
	}
}

// fileDigests returns the SHA-256 digest of the bytes of the device file and of the output file.
func fileDigests(t *testing.T, dir string) [2][sha256.Size]byte {
	t.Helper()
	var out [2][sha256.Size]byte
	for n, name := range []string{device.DeviceFile, device.OutputFile} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		out[n] = sha256.Sum256(data)
	}
	return out
}

// MIG-VER-1, MIG-VER-2, MIG-VER-4 and DEV-MIG-3: the reader of this version opens the two files that
// release 0.6.0 wrote. The reads through the read context of that release give the content of the release.
// The open, the reads and the close do not change a byte of the two files.
func TestRelease060ReadsDoNotChangeTheFiles(t *testing.T) {
	var contexts map[string]map[string]string
	var bundle struct {
		Roots       []string `json:"roots"`
		Descriptors []string `json:"descriptors"`
	}
	releaseFile(t, &contexts, "device", "contexts.json")
	releaseFile(t, &bundle, "bundles", "x1.json")
	contextID := contexts["device"]["dc1"]
	artifact := ""
	for _, text := range bundle.Descriptors {
		if strings.Contains(text, `"kind":"artifact"`) {
			artifact = digest([]byte(text)) // IDN-CAN-2: the bundle has the canonical form
		}
	}
	if contextID == "" || len(bundle.Roots) != 1 || artifact == "" {
		t.Fatalf("the release store has no context, no root or no artifact: %q, %v, %q", contextID, bundle.Roots, artifact)
	}
	dir := t.TempDir()
	for _, name := range []string{device.DeviceFile, device.OutputFile} {
		data, err := os.ReadFile(filepath.Join(release060, "device", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	before := fileDigests(t, dir)
	dev, out, err := device.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	registry := conformance.Registry()
	reader := ops.New(ops.Device, dev, registry)
	reader.Output = ops.New(ops.Output, out, registry)
	result := call(t, reader, "get", fields{"context": contextID, "refs": []string{bundle.Roots[0], artifact}}, "ok")
	if len(result.Items) != 2 {
		t.Errorf("get gave %s", result.JSON())
	}
	readEntry(t, reader, contextID, artifact, "Check the pump seal every week.\n")
	if found := call(t, reader, "search_text", fields{"context": contextID, "query": "seal"}, "ok"); len(found.Items) != 1 {
		t.Errorf("search_text gave %s", found.JSON())
	}
	if fileDigests(t, dir) != before {
		t.Error("the open and the reads changed a file of release 0.6.0 (DEV-MIG-3)")
	}
	if err := out.Close(); err != nil {
		t.Error(err)
	}
	if err := dev.Close(); err != nil {
		t.Error(err)
	}
	if fileDigests(t, dir) != before {
		t.Error("the close changed a file of release 0.6.0 (DEV-MIG-3)")
	}
}
