package pg

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/mohanraj00/aavanam/backend/core/ops"
)

// fingerprints has, for each table of the fingerprint, a query that gives one text for each row in the
// order of the key. The digest of the bytes stands for the bytes. The read contexts, the gates, the
// sections and the lock rows are not in the fingerprint.
var fingerprints = []struct{ table, query string }{
	{"objects", `SELECT encode(ref, 'hex') || ' ' || kind || ' ' || encode(sha256(canonical), 'hex') FROM objects ORDER BY ref`},
	{"edges", `SELECT encode(src, 'hex') || ' ' || encode(dst, 'hex') FROM edges ORDER BY src, dst`},
	{"payloads", `SELECT encode(digest, 'hex') || ' ' || size || ' ' || encode(sha256(data), 'hex') FROM payloads ORDER BY digest`},
	{"deferred", `SELECT encode(digest, 'hex') || ' ' || encode(ref, 'hex') FROM deferred ORDER BY digest, ref`},
	{"candidates", `SELECT encode(id, 'hex') || ' ' || encode(snapshot, 'hex') || ' ' || encode(collection, 'hex') || ' ' ||
		encode(policy, 'hex') || ' ' || coalesce(encode(expected_head, 'hex'), '-') FROM candidates ORDER BY id`},
	{"heads", `SELECT encode(collection, 'hex') || ' ' || encode(snapshot, 'hex') FROM heads ORDER BY collection`},
	{"publications", `SELECT seq || ' ' || encode(collection, 'hex') || ' ' || encode(candidate, 'hex') || ' ' || encode(receipt, 'hex') ||
		' ' || coalesce(encode(activation, 'hex'), '-') || ' ' || coalesce(encode(previous_head, 'hex'), '-') || ' ' ||
		encode(head, 'hex') FROM publications ORDER BY seq`},
	{"producer_objects", `SELECT encode(producer, 'hex') || ' ' || encode(key, 'hex') || ' ' || encode(sha256(data), 'hex')
		FROM producer_objects ORDER BY producer, key`},
	{"producer_entries", `SELECT encode(producer, 'hex') || ' ' || encode(log, 'hex') || ' ' || sequence || ' ' ||
		encode(sha256(entry), 'hex') FROM producer_entries ORDER BY producer, log, sequence`},
	{"producer_removals", `SELECT encode(producer, 'hex') || ' ' || sequence || ' ' || encode(key, 'hex') || ' ' || time || ' ' ||
		encode(reason, 'hex') FROM producer_removals ORDER BY producer, sequence`},
}

// Fingerprint returns a digest of the content, the catalog and the producer records of the store, from one
// snapshot of the database. Two equal stores have equal fingerprints. A failure is a storage fault.
func (s *Store) Fingerprint() string {
	ctx := context.Background()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		ops.Raise(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	h := sha256.New()
	v := view{ctx, tx}
	for _, f := range fingerprints {
		v.each(func(rows pgx.Rows) error {
			var line string
			if err := rows.Scan(&line); err != nil {
				return err
			}
			fmt.Fprintf(h, "%s:%s\n", f.table, line)
			return nil
		}, f.query)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Truncate removes each row of the store. Only the conformance driver and the tests use it: a service
// has no operation that removes content.
func (s *Store) Truncate(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `TRUNCATE `+strings.Join(tables, ", ")+` RESTART IDENTITY`)
	return err
}
