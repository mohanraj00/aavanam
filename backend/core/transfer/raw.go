package transfer

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"sort"

	"github.com/mohanraj00/aavanam/backend/core/canon"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/ops"
)

const rawFormat = "aavanam-bundle-raw/v1" // BUN-RAW-1

// isEntry checks one entry of the `payloads` of a raw manifest (BUN-RAW-1).
func isEntry(v jsontext.Value) bool {
	if v.Kind != jsontext.Object || len(v.Obj) != 2 {
		return false
	}
	digest, hasDigest := v.Get("digest")
	size, hasSize := v.Get("size")
	return hasDigest && hasSize && ops.IsRef(digest) && ops.IsCount(0)(size)
}

// WriteRaw writes a raw form of a bundle (BUN-RAW-3): the manifest line, then the bytes of each payload in
// ascending digest order. It reads no store. The bundle has the form of BUN-MAN-1.
func WriteRaw(w io.Writer, bundle jsontext.Value) error {
	if !isBundle(bundle) {
		return errors.New("the value is not a bundle (BUN-MAN-1)")
	}
	payloads := field(bundle, "payloads").Obj
	data := make(map[string][]byte, len(payloads))
	digests := make([]string, 0, len(payloads))
	for _, mem := range payloads {
		decoded, err := decodeText(mem.Val)
		if err != nil {
			return err
		}
		data[mem.Key] = decoded
		digests = append(digests, mem.Key)
	}
	sort.Strings(digests)
	entries := make([]jsontext.Value, len(digests))
	for n, digest := range digests {
		entries[n] = jsontext.O(jsontext.M("digest", jsontext.S(digest)), jsontext.M("size", jsontext.N(float64(len(data[digest])))))
	}
	manifest := make([]jsontext.Member, 0, len(bundle.Obj))
	for _, mem := range bundle.Obj {
		switch mem.Key {
		case "format":
			mem.Val = jsontext.S(rawFormat)
		case "payloads":
			mem.Val = jsontext.A(entries)
		}
		manifest = append(manifest, mem)
	}
	// The canonical form of a JSON value has no line feed byte (IDN-CAN-1).
	if _, err := w.Write(append(canon.Canonical(jsontext.O(manifest...)), '\n')); err != nil {
		return err
	}
	for _, digest := range digests {
		if _, err := w.Write(data[digest]); err != nil {
			return err
		}
	}
	return nil
}

func decodeText(v jsontext.Value) (data []byte, err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("a payload of the bundle is not base64 text (BUN-MAN-1)")
		}
	}()
	return ops.Unb64(v), nil
}

// rawParts checks a raw bundle in the order of BUN-RAW-4. It returns the manifest and the bytes of each
// entry. It reads the manifest line, then one entry at a time, and it checks the digest of an entry before
// it reads the next entry. A check that fails stops the operation with one reason.
func rawParts(raw io.Reader) (jsontext.Value, map[string][]byte) {
	r := bufio.NewReaderSize(raw, 1<<16)
	line, err := r.ReadBytes('\n')
	if err == io.EOF {
		ops.Fail("malformed_json") // step 1: no line feed byte
	}
	if err != nil {
		ops.Raise(err)
	}
	manifest, formError := jsontext.Parse(line[:len(line)-1], jsontext.Strict)
	if formError != nil {
		ops.Fail(formError.Codes[0])
	}
	entries := field(manifest, "payloads")
	if !isManifest(manifest, rawFormat) || !ops.ListOf(isEntry, true)(entries) { // step 2
		ops.Fail("field_type")
	}
	withheld := map[string]bool{}
	for _, digest := range field(manifest, "withheld").Arr {
		withheld[digest.Str] = true
	}
	data := make(map[string][]byte, len(entries.Arr))
	for _, entry := range entries.Arr {
		digest := field(entry, "digest").Str
		if _, twice := data[digest]; twice || withheld[digest] {
			ops.Fail("field_type")
		}
		data[digest] = nil
	}
	for _, entry := range entries.Arr { // step 3
		digest := field(entry, "digest").Str
		size, _ := field(entry, "size").Int()
		// The size comes from the input. Thus the buffer grows only with the bytes that the input has.
		var piece bytes.Buffer
		n, err := io.CopyN(&piece, r, size)
		if err != nil && err != io.EOF {
			ops.Raise(err)
		}
		if n != size {
			ops.Fail("field_type")
		}
		if jsontext.Digest(piece.Bytes()) != digest {
			ops.Fail("digest_mismatch")
		}
		data[digest] = piece.Bytes()
	}
	if _, err := r.ReadByte(); err == nil { // step 4
		ops.Fail("field_type")
	} else if err != io.EOF {
		ops.Raise(err)
	}
	return manifest, data
}
