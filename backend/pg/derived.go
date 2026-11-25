package pg

import (
	"errors"
	"fmt"
	"sort"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/mohanraj00/aavanam/backend/core/canon"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/ops"
)

// The text index (ops.TextIndex, docs/design/backend.md, section 4).
//
// A row of `text_tokens` is the set of the word tokens of one text payload, as a bit set: each token sets
// `probes` bits. A digest is a candidate of a query if each bit of each query token is set. Equal words
// give equal tokens and equal bits, so the index drops no digest that has a match. Different tokens can
// set the same bits. Then the scan of the bytes removes the false candidate (CRD-TXT-3). The set keeps no
// word order and no count.
//
// Reason for a bit set: one row for each word of each payload is larger than the payload bytes, and the
// size target of the design is 1.5 times the payload bytes for all relations.
//
// A set has 2 bytes for each different token. With 4 bits for each token, a token that the payload does not
// have is a false candidate for about 1 payload in 400. A set of 8 KiB or less stays in the row
// (`STORAGE MAIN`), so the statement reads no out-of-line value for a payload of about 4 000 different words or less.
const (
	probes        = 4       // the bits of one token
	bytesPerToken = 2       // the bytes of a set for each different token
	minBytes      = 64      // the least size of a set
	maxBytes      = 8 << 20 // the largest size of a set
)

// tokenValue returns the number of a token of package text: the letter `t` and 16 hexadecimal digits.
func tokenValue(token string) uint64 {
	if len(token) != 17 {
		ops.Raise(fmt.Errorf("the token %q does not have 17 characters", token))
	}
	value, err := strconv.ParseUint(token[1:], 16, 64)
	if err != nil {
		ops.Raise(err)
	}
	return value
}

// positions returns the bit positions of a token, before the reduction to the size of a set: the position
// in a set is the remainder of the value and the number of bits of the set.
func positions(token string) [probes]int64 {
	value := tokenValue(token)
	low, high := value&0xffffffff, value>>32|1
	var out [probes]int64
	for i := range out {
		out[i] = int64(low + uint64(i)*high) // less than 2^35
	}
	return out
}

// bitSet returns the bit set of the tokens of a payload. Bit n is bit n%8 of byte n/8, from the least
// significant bit, as the SQL function get_bit reads it.
func bitSet(tokens []string) []byte {
	different := make(map[string]struct{}, len(tokens))
	for _, token := range tokens {
		different[token] = struct{}{}
	}
	bits := make([]byte, min(max(len(different)*bytesPerToken, minBytes), maxBytes))
	size := int64(len(bits)) * 8
	for token := range different {
		for _, p := range positions(token) {
			p %= size
			bits[p/8] |= 1 << (p % 8)
		}
	}
	return bits
}

func (v view) PutTextTokens(digest string, tokens []string) {
	v.exec(`INSERT INTO text_tokens (digest, bits) VALUES ($1, $2) ON CONFLICT DO NOTHING`, must(digest), bitSet(tokens))
}

// tokenBatch has the text index rows of one update. The update writes them in one statement for each
// tokenBatchBytes of bit sets, and not in one statement for each payload.
type tokenBatch struct {
	rows map[string][]byte // digest, then bit set
	size int
}

const tokenBatchBytes = 4 << 20

// PutTextTokens keeps the row for the next flush. A read of the index in the update writes the rows first.
func (t tx) PutTextTokens(digest string, tokens []string) {
	must(digest)
	if _, ok := t.tokens.rows[digest]; ok {
		return
	}
	if t.tokens.rows == nil {
		t.tokens.rows = map[string][]byte{}
	}
	bits := bitSet(tokens)
	t.tokens.rows[digest] = bits
	if t.tokens.size += len(bits); t.tokens.size >= tokenBatchBytes {
		t.flushTokens()
	}
}

// flushTokens writes the kept rows in ascending order of the digest. Thus two updates that write the same
// rows take the row locks in one order and cannot wait for each other.
func (t tx) flushTokens() {
	if len(t.tokens.rows) == 0 {
		return
	}
	digests := make([]string, 0, len(t.tokens.rows))
	for digest := range t.tokens.rows {
		digests = append(digests, digest)
	}
	sort.Strings(digests)
	sets := make([][]byte, len(digests))
	for i, digest := range digests {
		sets[i] = t.tokens.rows[digest]
	}
	t.tokens.rows, t.tokens.size = nil, 0
	t.exec(`INSERT INTO text_tokens (digest, bits) SELECT * FROM unnest($1::bytea[], $2::bytea[]) ON CONFLICT DO NOTHING`,
		raws(digests), sets)
}

func (t tx) TextIndexed(digest string) bool {
	t.flushTokens()
	return t.view.TextIndexed(digest)
}

func (t tx) TextCandidates(tokens, digests []string) (candidates, unindexed []string) {
	t.flushTokens()
	return t.view.TextCandidates(tokens, digests)
}

func (v view) TextIndexed(digest string) bool {
	key, ok := raw(digest)
	return ok && v.exists(`SELECT 1 FROM text_tokens WHERE digest = $1`, key)
}

// TextCandidates reads the bit set of each digest of the list in one statement. The statement has the bit
// positions of the tokens, one condition for each position, and no text of the request (CRD-TXT-3).
func (v view) TextCandidates(tokens, digests []string) (candidates, unindexed []string) {
	if len(digests) == 0 {
		return nil, nil
	}
	args := []any{keys(digests)}
	condition := "true"
	seen := map[int64]bool{}
	for _, token := range tokens {
		for _, p := range positions(token) {
			if !seen[p] {
				seen[p] = true
				args = append(args, p)
				condition += fmt.Sprintf(" AND get_bit(bits, ($%d::bigint %% (octet_length(bits)::bigint * 8))::int) = 1", len(args))
			}
		}
	}
	indexed := make(map[string]bool, len(digests))
	v.each(func(rows pgx.Rows) error {
		var digest []byte
		var possible bool
		if err := rows.Scan(&digest, &possible); err != nil {
			return err
		}
		indexed[name(digest)] = true
		if possible {
			candidates = append(candidates, name(digest))
		}
		return nil
	}, `SELECT digest, `+condition+` FROM text_tokens WHERE digest = ANY($1::bytea[])`, args...)
	for _, digest := range digests {
		if !indexed[digest] {
			unindexed = append(unindexed, digest)
		}
	}
	return candidates, unindexed
}

// The row cache (ops.RowCache): `table_heads` has the declared columns and the number of rows of a table
// payload, and `table_rows` has its rows. The head and the rows of one table come in one update.

// rowBatch is the largest number of row bytes of one INSERT statement.
const rowBatch = 4 << 20

func (t tx) PutTable(digest string, columns []string, rows [][]byte) {
	key := must(digest)
	tag, err := t.q.Exec(t.ctx, `INSERT INTO table_heads (digest, columns, count) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
		key, canon.Canonical(jsontext.Strings(columns)), len(rows))
	if err != nil {
		ops.Raise(err)
	}
	if tag.RowsAffected() == 0 {
		return // the store has the table, or a different update wrote it
	}
	for start := 0; start < len(rows); {
		end, size := start, 0
		for end < len(rows) && (end == start || size+len(rows[end]) <= rowBatch) {
			size += len(rows[end])
			end++
		}
		t.exec(`INSERT INTO table_rows (digest, idx, cells) SELECT $1, $2 + u.n - 1, u.cells
			FROM unnest($3::bytea[]) WITH ORDINALITY AS u(cells, n)`, key, start, rows[start:end])
		start = end
	}
}

// PutTable has an effect only in an update.
func (s session) PutTable(string, []string, [][]byte) {}

func (v view) TableHead(digest string) ([]string, int, bool) {
	key, ok := raw(digest)
	if !ok {
		return nil, 0, false
	}
	var columns []byte
	var count int
	if !v.row([]any{&columns, &count}, `SELECT columns, count FROM table_heads WHERE digest = $1`, key) {
		return nil, 0, false
	}
	value, err := jsontext.Parse(columns, jsontext.Strict)
	if err != nil {
		ops.Raise(errors.New("the columns of a table of the row cache are not JSON"))
	}
	return ops.Strings(value), count, true
}

func (v view) TableRow(digest string, index int) ([]byte, bool) {
	key, ok := raw(digest)
	if !ok {
		return nil, false
	}
	var cells []byte
	if !v.row([]any{&cells}, `SELECT cells FROM table_rows WHERE digest = $1 AND idx = $2`, key, index) {
		return nil, false
	}
	return cells, true
}

func (v view) TableRows(digest string) [][]byte {
	key, ok := raw(digest)
	if !ok {
		return nil
	}
	var out [][]byte
	v.each(func(rows pgx.Rows) error {
		var cells []byte
		if err := rows.Scan(&cells); err != nil {
			return err
		}
		out = append(out, cells)
		return nil
	}, `SELECT cells FROM table_rows WHERE digest = $1 ORDER BY idx`, key)
	return out
}
