// Package synth makes a synthetic store of a declared size from a fixed seed (docs/design/backend.md,
// section 7). The data is neutral: pseudo-words from a fixed syllable set, numbers and random bytes. The
// bytes of each payload come from the seed and the index of the payload only. Thus two runs with one shape
// and one seed send the same requests in the same order, and make the same store.
//
// The store has one main collection, `c000`. Its head is the root of the measured read context: it has
// the text artifacts of the search, the 64 KiB artifacts and the largest table. The other collections
// hold the rest of the declared descriptors and bytes.
package synth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"

	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/ops"
)

// Shape is the declared size of a store (docs/design/backend.md, section 7).
type Shape struct {
	Name          string
	Descriptors   int   // the descriptors of the store
	PayloadBytes  int64 // the payload bytes of the store
	Collections   int   // each collection has a head
	Members       int   // the target members of the context of the main head
	TextArtifacts int   // the text artifacts of the context of the main head
	TableRows     int   // the rows of the largest table
	TableBytes    int64 // the payload bytes of the largest table
}

// The shapes of the design, and a small shape for tests.
var (
	S0   = Shape{"S0", 20_000, 200 << 20, 10, 2_000, 2_000, 10_000, 2 << 20}
	S1   = Shape{"S1", 200_000, 2 << 30, 50, 20_000, 2_000, 100_000, 20 << 20}
	Tiny = Shape{"tiny", 600, 4 << 20, 3, 120, 60, 300, 32 << 10}
)

// ShapeOf returns the shape of a name.
func ShapeOf(name string) (Shape, bool) {
	for _, s := range []Shape{S0, S1, Tiny} {
		if strings.EqualFold(s.Name, name) {
			return s, true
		}
	}
	return Shape{}, false
}

// The fixed values of the generator.
const (
	UnitArtifacts = 20       // the artifacts of one knowledge unit
	WideBytes     = 64 << 10 // the size of a wide artifact, which the 64 KiB read measures
	wideEvery     = 50       // each 50th artifact is wide
	matchEvery    = 100      // each 100th text artifact of the main context has the query phrase
	changeEvery   = 100      // an update changes each 100th artifact (1%)
	changeAt      = 37       // the position of a changed artifact in its group of 100
	groups        = 100      // the values of the table column `group`
	regions       = 4        // the values of the overlay key `region`
	unitOverhead  = 3        // the binding set, the knowledge unit and the overlay of a unit

	// Query is the phrase of the text search: two words that the syllable set cannot make.
	Query = "amber harbor"
	// Group is the value of the `where` filter of `query_data`. It selects 1% of the rows.
	Group = "g007"
	// Region is the value of the export filter. It selects one unit in four.
	Region = "r0"
	// Band is the value of the overlay key `band` of the first BandUnits units of the store, in the order
	// of the collections. A filtered export with this value has that number of knowledge-unit roots.
	Band      = "b0"
	BandUnits = 500
	// Policy is the acceptance policy of each candidate. The bench accepts each candidate.
	Policy = "sample/accept/v1"
)

// Store is the description of a store that Build made.
type Store struct {
	Shape Shape
	Seed  uint64

	Collections []string
	Heads       []string // the head snapshot of each collection, in the order of Collections
	Table       string   // the largest table, in the main context
	Wide        []string // the 64 KiB text artifacts of the main context
	Matches     int      // the items of a search for Query in the main context

	Descriptors   int    // the descriptors that the requests wrote
	PayloadBytes  int64  // the payload bytes that the requests wrote
	Members       int    // the members of the context of the main head
	TextArtifacts int    // the text artifacts of the main context
	GroupRows     int    // the rows with the value Group
	RegionUnits   int    // the units of the main collection with the value Region
	BandUnits     int    // the units of the store with the value Band
	Digest        string // the SHA-256 digest of the requests, in order

	units   [][]*unit // the units of each collection
	sizes   []int     // the size of each artifact of the first version
	version int
	digest  hash.Hash
	vocab   []string
	nextArt int // the index of the next new artifact
	made    int // the units that the store made, for the value of `band`
}

type unit struct {
	id        string
	artifacts []int    // the global index of each artifact
	refs      []string // the reference of each artifact
	table     bool     // the binding set of the unit binds the table
	band      string   // the value of the overlay key `band`
	bs, ku    string
	overlay   string
}

// Summary is the part of a Store that a result file records.
type Summary struct {
	Shape         string `json:"shape"`
	Seed          uint64 `json:"seed"`
	Collections   int    `json:"collections"`
	Descriptors   int    `json:"descriptors"`
	PayloadBytes  int64  `json:"payload_bytes"`
	Members       int    `json:"main_context_members"`
	TextArtifacts int    `json:"main_context_text_artifacts"`
	TableRows     int    `json:"table_rows"`
	TableBytes    int64  `json:"table_bytes"`
	Matches       int    `json:"search_matches"`
	GroupRows     int    `json:"query_data_rows"`
	RegionUnits   int    `json:"filtered_units"`
	BandUnits     int    `json:"band_units"`
	Digest        string `json:"request_digest"`
}

// band returns the value of the overlay key `band` of the next new unit.
func (s *Store) band() string {
	s.made++
	if s.made <= BandUnits {
		s.BandUnits++
		return Band
	}
	return "b1"
}

// Summary returns the record of the store.
func (s *Store) Summary() Summary {
	return Summary{s.Shape.Name, s.Seed, len(s.Collections), s.Descriptors, s.PayloadBytes, s.Members,
		s.TextArtifacts, s.Shape.TableRows, s.Shape.TableBytes, s.Matches, s.GroupRows, s.RegionUnits,
		s.BandUnits, hex.EncodeToString(s.digest.Sum(nil))}
}

// Main returns the main collection.
func (s *Store) Main() string { return s.Collections[0] }

// plan gives the number of units of each collection, and the extra artifacts of the last unit, so that
// the store has exactly the declared descriptors.
func plan(shape Shape) (units []int, extra int, err error) {
	per := UnitArtifacts + unitOverhead
	fixed := 2 + 3*shape.Collections // the schema, the table, and a receipt, an overlay set and a snapshot for each collection
	// The main context has the snapshot, the overlay set, the table and its schema, and its units.
	main := max((shape.TextArtifacts+UnitArtifacts-1)/UnitArtifacts, (shape.Members-4)/per)
	rest := shape.Descriptors - fixed - main*per
	if shape.Collections < 2 || rest < (shape.Collections-1)*per {
		return nil, 0, fmt.Errorf("shape %s has too few descriptors for its collections", shape.Name)
	}
	others := rest / per
	units = []int{main}
	for c := 1; c < shape.Collections; c++ {
		n := others / (shape.Collections - 1)
		if c-1 < others%(shape.Collections-1) {
			n++
		}
		units = append(units, n)
	}
	return units, rest - others*per, nil
}

// Build makes the store of a shape in an empty writer store. It declares each collection a `serving`
// collection, so the store must have had no call before. It registers the policy Policy.
func Build(ctx context.Context, store *ops.Store, shape Shape, seed uint64) (*Store, error) {
	units, extra, err := plan(shape)
	if err != nil {
		return nil, err
	}
	s := &Store{Shape: shape, Seed: seed, digest: sha256.New(), vocab: vocabulary(), version: 1}
	for c := range units {
		name := fmt.Sprintf("c%03d", c)
		s.Collections = append(s.Collections, name)
		if err := store.Section(ctx, name, "serving"); err != nil {
			return nil, err
		}
	}
	store.Accept(Policy, func(context.Context, string, string, string) ops.Decision { return ops.Accepted })

	// Each artifact has its size before the first request, so that the store has the declared bytes.
	total := extra
	for _, n := range units {
		total += n * UnitArtifacts
	}
	smallBytes := int64(len(schemaText)) + int64(len(units))*int64(len(receiptText("c000", 1)))
	s.sizes = artifactSizes(total, shape.PayloadBytes-shape.TableBytes-smallBytes)
	s.nextArt = total
	sizes := s.sizes

	w := &writer{ctx: ctx, store: store, s: s}
	schema := w.put([]desc{{value: map[string]any{"kind": "schema", "schema_profile": "sample/schema/v1"},
		payload: []byte(schemaText), media: "application/json"}})[0]
	table, rows := tableText(shape, seed)
	s.GroupRows = rows
	s.Table = w.put([]desc{{value: map[string]any{"kind": "data-resource", "data_profile": "aavanam/table/v1",
		"schema": schema}, payload: table, media: "application/json"}})[0]

	next := 0
	for c, n := range units {
		var list []*unit
		for u := range n {
			x := &unit{id: fmt.Sprintf("%s-u%04d", s.Collections[c], u), table: c == 0 && u == 0, band: s.band()}
			count := UnitArtifacts
			if c == len(units)-1 && u == n-1 {
				count += extra
			}
			for range count {
				x.artifacts = append(x.artifacts, next)
				next++
			}
			list = append(list, x)
		}
		s.units = append(s.units, list)
		var arts []desc
		for _, x := range list {
			for _, a := range x.artifacts {
				arts = append(arts, s.artifact(c, a, sizes[a], 1))
			}
		}
		refs := w.put(arts)
		for _, x := range list {
			x.refs, refs = refs[:len(x.artifacts)], refs[len(x.artifacts):]
		}
		head, err := w.publish(c, list, "", list)
		if err != nil {
			return nil, err
		}
		s.Heads = append(s.Heads, head)
	}
	if w.err != nil {
		return nil, w.err
	}
	main := s.units[0]
	s.Members = 4 + len(main)*unitOverhead // the snapshot, the overlay set, the table and its schema
	for _, x := range main {
		s.Members += len(x.artifacts)
		for _, a := range x.artifacts {
			if a < shape.TextArtifacts {
				s.TextArtifacts++
				if a%wideEvery == 0 {
					s.Wide = append(s.Wide, x.refs[a-x.artifacts[0]])
				}
				if a%matchEvery == matchEvery/2 {
					s.Matches++
				}
			}
		}
	}
	for u := range main {
		if u%regions == 0 {
			s.RegionUnits++
		}
	}
	return s, nil
}

// Update makes a second version of each collection, in which each 100th artifact has new bytes of the same
// size (1% of the artifacts). Each changed unit gets a new binding set, a new knowledge unit with the same
// logical ID and a new overlay. Each collection gets a new head. It returns the number of new descriptors.
func Update(ctx context.Context, store *ops.Store, s *Store) (int, error) {
	s.version++
	w := &writer{ctx: ctx, store: store, s: s}
	before := s.Descriptors
	for c, list := range s.units {
		var arts []desc
		var where [][2]int
		for u, x := range list {
			for i, a := range x.artifacts {
				if a%changeEvery == changeAt {
					arts = append(arts, s.artifact(c, a, s.sizes[a], s.version))
					where = append(where, [2]int{u, i})
				}
			}
		}
		refs := w.put(arts)
		changed := map[int]bool{}
		for n, p := range where {
			list[p[0]].refs[p[1]] = refs[n]
			changed[p[0]] = true
		}
		var touched []*unit
		for u := range list {
			if changed[u] {
				touched = append(touched, list[u])
			}
		}
		if len(touched) == 0 {
			continue
		}
		head, err := w.publish(c, list, s.Heads[c], touched)
		if err != nil {
			return 0, err
		}
		s.Heads[c] = head
	}
	return s.Descriptors - before, w.err
}

// Timing is the time of each step of one publication.
type Timing struct {
	Descriptors int           // the new descriptors of the candidate, without its snapshot
	Put         time.Duration // the `put` calls of the new descriptors
	Snapshot    time.Duration
	Candidate   time.Duration
	Publish     time.Duration
}

// AddUnits writes 40 new units to the main collection, 1 000 new descriptors in total (22 small text
// artifacts, a binding set, a knowledge unit and an overlay for each unit), and publishes a new head with
// the old units and the new units. It returns the time of each step.
func AddUnits(ctx context.Context, store *ops.Store, s *Store) (Timing, error) {
	const newUnits, perUnit, size = 40, 22, 2 << 10
	s.version++
	w := &writer{ctx: ctx, store: store, s: s}
	before := s.Descriptors
	var fresh []*unit
	var arts []desc
	for u := range newUnits {
		x := &unit{id: fmt.Sprintf("%s-n%02d-%02d", s.Main(), s.version, u), band: s.band()}
		for range perUnit {
			x.artifacts = append(x.artifacts, s.nextArt)
			arts = append(arts, s.artifact(0, s.nextArt, size, s.version))
			s.nextArt++
		}
		fresh = append(fresh, x)
	}
	var t Timing
	w.timing = &t
	refs := w.put(arts)
	for _, x := range fresh {
		x.refs, refs = refs[:perUnit], refs[perUnit:]
	}
	s.units[0] = append(s.units[0], fresh...)
	head, err := w.publish(0, s.units[0], s.Heads[0], fresh)
	if err != nil {
		return t, err
	}
	s.Heads[0] = head
	t.Descriptors = s.Descriptors - before - 3 // without the receipt, the overlay set and the snapshot
	return t, w.err
}

// desc is one descriptor of a `put` request, with the bytes of its payload, if it has one.
type desc struct {
	value   map[string]any
	payload []byte
	media   string
}

type writer struct {
	ctx    context.Context
	store  *ops.Store
	s      *Store
	err    error
	timing *Timing
}

// call sends one request and returns the items of an `ok` result.
func (w *writer) call(operation string, request any) []map[string]any {
	if w.err != nil {
		return nil
	}
	body, err := json.Marshal(request)
	if err != nil {
		w.err = err
		return nil
	}
	w.s.digest.Write([]byte(operation))
	w.s.digest.Write(body)
	out := w.store.Call(w.ctx, jsontext.S(operation), body).JSON()
	var result struct {
		Status  string           `json:"status"`
		Reasons []map[string]any `json:"reasons"`
		Items   []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		w.err = err
		return nil
	}
	if result.Status != "ok" {
		w.err = fmt.Errorf("%s gave %s: %s", operation, result.Status, truncate(out))
		return nil
	}
	return result.Items
}

func truncate(b []byte) string {
	if len(b) > 600 {
		return string(b[:600]) + "..."
	}
	return string(b)
}

// maxBatch is the limit of the payload bytes of one `put` request.
const maxBatch = 8 << 20

// put writes descriptors in batches and returns the reference of each.
func (w *writer) put(list []desc) []string {
	var refs []string
	for len(list) > 0 && w.err == nil {
		n, bytes := 0, 0
		for n < len(list) && n < 500 && (n == 0 || bytes+len(list[n].payload) <= maxBatch) {
			bytes += len(list[n].payload)
			n++
		}
		texts := make([]string, n)
		payloads := map[string]string{}
		for i, d := range list[:n] {
			value := map[string]any{"descriptor_profile": "aavanam/base/v1"}
			for k, v := range d.value {
				value[k] = v
			}
			if d.payload != nil {
				digest := digestOf(d.payload)
				value["payload"] = map[string]any{"digest": digest, "media_type": d.media, "size": len(d.payload)}
				payloads[digest] = base64.StdEncoding.EncodeToString(d.payload)
				w.s.PayloadBytes += int64(len(d.payload))
			}
			text, err := json.Marshal(value)
			if err != nil {
				w.err = err
				return nil
			}
			texts[i] = string(text)
		}
		start := time.Now()
		items := w.call("put", map[string]any{"descriptors": texts, "payloads": payloads})
		if w.timing != nil {
			w.timing.Put += time.Since(start)
		}
		for _, item := range items {
			ref, _ := item["ref"].(string)
			refs = append(refs, ref)
		}
		w.s.Descriptors += n
		list = list[n:]
	}
	if w.err == nil && len(refs) == 0 && len(list) > 0 {
		w.err = fmt.Errorf("put gave no references")
	}
	return refs
}

// publish writes the binding sets, the knowledge units and the overlays of the touched units, an overlay
// set with the overlay of each unit of the list, a receipt and a snapshot, and publishes the snapshot.
func (w *writer) publish(c int, list []*unit, expected string, touched []*unit) (string, error) {
	s := w.s
	var sets []desc
	for _, x := range touched {
		bindings := map[string]any{}
		for i, ref := range x.refs {
			bindings[fmt.Sprintf("a%02d", i)] = ref
		}
		if x.table {
			bindings["table"] = s.Table
		}
		sets = append(sets, desc{value: map[string]any{"kind": "binding-set", "bindings": bindings}})
	}
	for i, ref := range w.put(sets) {
		touched[i].bs = ref
	}
	var kus []desc
	for _, x := range touched {
		kus = append(kus, desc{value: map[string]any{"kind": "knowledge-unit", "logical_id": x.id, "binding_set": x.bs}})
	}
	for i, ref := range w.put(kus) {
		touched[i].ku = ref
	}
	var overlays []desc
	for _, x := range touched {
		u := indexOf(list, x)
		overlays = append(overlays, desc{value: map[string]any{"kind": "overlay", "subject": x.ku,
			"origin": "sample-producer", "values": map[string]any{"region": "r" + strconv.Itoa(u%regions), "band": x.band}}})
	}
	for i, ref := range w.put(overlays) {
		touched[i].overlay = ref
	}
	var members, roots []string
	for _, x := range list {
		members = append(members, x.overlay)
		roots = append(roots, x.ku)
	}
	set := w.put([]desc{{value: map[string]any{"kind": "overlay-set", "members": members}}})
	receipt := w.put([]desc{{value: map[string]any{"kind": "receipt", "producer": "sample-producer"},
		payload: []byte(receiptText(s.Collections[c], s.version)), media: "text/plain"}})
	if w.err != nil {
		return "", w.err
	}
	start := time.Now()
	items := w.call("create_snapshot", map[string]any{"collection": s.Collections[c], "units": roots, "overlay_sets": set})
	snapshotTime := time.Since(start)
	if w.err != nil {
		return "", w.err
	}
	s.Descriptors++
	snapshot, _ := items[0]["ref"].(string)
	var head any
	if expected != "" {
		head = expected
	}
	start = time.Now()
	items = w.call("create_candidate", map[string]any{"snapshot": snapshot, "checks": []string{}, "policy": Policy, "expected_head": head})
	candidateTime := time.Since(start)
	if w.err != nil {
		return "", w.err
	}
	candidate, _ := items[0]["candidate"].(string)
	if ref, ok := items[0]["ref"].(string); ok && candidate == "" {
		candidate = ref
	}
	start = time.Now()
	w.call("publish", map[string]any{"candidate": candidate, "receipt": receipt[0]})
	if w.timing != nil {
		w.timing.Snapshot, w.timing.Candidate, w.timing.Publish = snapshotTime, candidateTime, time.Since(start)
	}
	return snapshot, w.err
}

func indexOf(list []*unit, x *unit) int {
	for i, y := range list {
		if y == x {
			return i
		}
	}
	return -1
}

func digestOf(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

const schemaText = `{"fields": ["key", "value", "group", "note"]}`

func receiptText(collection string, version int) string {
	return fmt.Sprintf("accepted %s version %04d\n", collection, version)
}

// artifactSizes gives the size of each artifact: WideBytes for each 50th artifact, and an equal share of
// the other bytes for the rest. The last artifact gets the remainder.
func artifactSizes(count int, budget int64) []int {
	wide := (count + wideEvery - 1) / wideEvery
	rest := budget - int64(wide)*WideBytes
	each := rest / int64(count-wide)
	sizes := make([]int, count)
	for a := range sizes {
		if a%wideEvery == 0 {
			sizes[a] = WideBytes
		} else {
			sizes[a] = int(each)
		}
	}
	last := count - 1
	if last%wideEvery == 0 {
		last--
	}
	sizes[last] += int(rest - each*int64(count-wide))
	return sizes
}

// artifact returns the descriptor of one artifact. The first TextArtifacts artifacts and each artifact
// outside the main collection are text. The other artifacts of the main collection are random bytes.
func (s *Store) artifact(c, a, size, version int) desc {
	rng := rand.New(rand.NewPCG(s.Seed, uint64(a)<<16|uint64(version)))
	value := map[string]any{"kind": "artifact", "artifact_type": "sample/document/v1"}
	if c == 0 && a >= s.Shape.TextArtifacts && a < len(s.sizes) {
		data := make([]byte, size)
		for i := 0; i < size; i += 8 {
			v := rng.Uint64()
			for j := 0; j < 8 && i+j < size; j++ {
				data[i+j] = byte(v >> (8 * j))
			}
		}
		return desc{value: value, payload: data, media: "application/octet-stream"}
	}
	phrase := c == 0 && a < s.Shape.TextArtifacts && a%matchEvery == matchEvery/2
	return desc{value: value, payload: s.text(rng, fmt.Sprintf("item %d version %d\n", a, version), size, phrase), media: "text/plain"}
}

// text returns text of exactly size bytes: a header line, then lines of 12 words. If phrase is true, the
// text has Query one time, after the first line of words.
func (s *Store) text(rng *rand.Rand, header string, size int, phrase bool) []byte {
	b := make([]byte, 0, size+16)
	b = append(b, header...)
	for line := 0; len(b) < size; line++ {
		if phrase && line == 1 {
			b = append(b, Query...)
			b = append(b, ' ')
		}
		for w := range 12 {
			if w > 0 {
				b = append(b, ' ')
			}
			b = append(b, s.vocab[rng.IntN(len(s.vocab))]...)
		}
		b = append(b, '.', '\n')
	}
	b = b[:size]
	b[size-1] = '\n'
	return b
}

// vocabulary returns 4 096 pseudo-words of two or three syllables. The syllables have no letter b or h, so
// no word is a word of Query.
func vocabulary() []string {
	var syllables []string
	for _, c := range "klmnprstv" {
		for _, v := range "aeiou" {
			syllables = append(syllables, string(c)+string(v))
		}
	}
	var words []string
	for _, x := range syllables {
		for _, y := range syllables {
			words = append(words, x+y)
		}
	}
	for _, x := range syllables {
		for _, y := range syllables {
			for _, z := range syllables {
				if len(words) == 4096 {
					return words
				}
				words = append(words, x+y+z)
			}
		}
	}
	return words
}

// tableText returns the payload of the largest table, with exactly TableBytes bytes, and the number of
// rows with the value Group. The rows are `[key, value, group, note]`.
func tableText(shape Shape, seed uint64) ([]byte, int) {
	rng := rand.New(rand.NewPCG(seed, 1<<62))
	head := `{"columns": ["key", "value", "group", "note"], "key": ["key"], "rows": [`
	b := make([]byte, 0, shape.TableBytes)
	b = append(b, head...)
	fixed := len(`["k0000000", 1000000, "g000", ""], `)
	note := int(shape.TableBytes-int64(len(head))-2)/shape.TableRows - fixed
	matches := 0
	for r := range shape.TableRows {
		if r > 0 {
			b = append(b, ", "...)
		}
		group := fmt.Sprintf("g%03d", r%groups)
		if group == Group {
			matches++
		}
		b = fmt.Appendf(b, `["k%07d", %07d, "%s", "`, r, 1_000_000+rng.IntN(9_000_000), group)
		n := note
		if r == shape.TableRows-1 {
			n += int(shape.TableBytes) - len(b) - n - 4 // the closing `"]]}`
		}
		for range max(n, 0) {
			b = append(b, byte('a'+rng.IntN(26)))
		}
		b = append(b, `"]`...)
	}
	b = append(b, "]}"...)
	return b, matches
}
