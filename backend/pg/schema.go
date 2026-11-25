// Package pg is the PostgreSQL storage of the Go core (docs/design/backend.md, section 2).
//
// Each table that has source content takes inserts only. A trigger refuses an UPDATE and a DELETE on these
// tables. Each JSON string value of a request (a collection name, a policy name, a producer key, a context
// identity) is in a `bytea` column, because a PostgreSQL `text` value cannot have U+0000 and I-JSON permits
// it. A reference or a digest is a `bytea` value of 32 bytes. Its byte order is the order of `sha256:<hex>`.
//
// A string of a request has no length bound, and an entry of a B-tree index has one (about 2.7 KiB). Thus
// no index has such a string. Each index and each unique rule uses the SHA-256 digest of the string, and
// each statement also compares the full string.
// The schema has no `jsonb` column: `jsonb` changes numbers and removes duplicate keys.
package pg

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// migrations has the schema changes in order. A change never edits an earlier entry: a new change is a
// new entry.
var migrations = []string{`
CREATE TABLE objects (
    ref bytea PRIMARY KEY CHECK (octet_length(ref) = 32),
    kind text COLLATE "C" NOT NULL,
    collection bytea,
    canonical bytea NOT NULL
);
CREATE INDEX objects_snapshots ON objects ((sha256(collection))) WHERE kind = 'snapshot';
CREATE TABLE edges (
    src bytea NOT NULL,
    dst bytea NOT NULL,
    PRIMARY KEY (src, dst)
);
CREATE INDEX edges_dst ON edges (dst, src);
CREATE TABLE payloads (
    digest bytea PRIMARY KEY CHECK (octet_length(digest) = 32),
    size bigint NOT NULL,
    data bytea NOT NULL
);
CREATE TABLE deferred (
    digest bytea NOT NULL,
    ref bytea NOT NULL,
    PRIMARY KEY (digest, ref)
);
CREATE INDEX deferred_ref ON deferred (ref);
CREATE TABLE candidates (
    id bytea PRIMARY KEY,
    snapshot bytea NOT NULL,
    collection bytea NOT NULL,
    policy bytea NOT NULL,
    expected_head bytea
);
CREATE TABLE heads (
    collection bytea NOT NULL,
    snapshot bytea NOT NULL
);
CREATE UNIQUE INDEX heads_key ON heads ((sha256(collection)));
CREATE TABLE publications (
    seq bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    collection bytea NOT NULL,
    candidate bytea NOT NULL UNIQUE,
    receipt bytea NOT NULL,
    activation bytea,
    previous_head bytea,
    head bytea NOT NULL
);
CREATE INDEX publications_collection ON publications ((sha256(collection)), seq);
CREATE TABLE gates (collection bytea NOT NULL);
CREATE UNIQUE INDEX gates_key ON gates ((sha256(collection)));
CREATE TABLE sections (
    collection bytea NOT NULL,
    section text COLLATE "C" NOT NULL
);
CREATE UNIQUE INDEX sections_key ON sections ((sha256(collection)));
CREATE TABLE collection_locks (collection bytea NOT NULL);
CREATE UNIQUE INDEX collection_locks_key ON collection_locks ((sha256(collection)));
CREATE TABLE contexts (
    id bytea PRIMARY KEY,
    roots bytea[] NOT NULL,
    capabilities text[] NOT NULL,
    opened timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE context_members (
    context bytea NOT NULL,
    ref bytea NOT NULL,
    PRIMARY KEY (context, ref)
);
CREATE TABLE producer_objects (
    producer bytea NOT NULL,
    key bytea NOT NULL,
    data bytea NOT NULL
);
CREATE UNIQUE INDEX producer_objects_key ON producer_objects ((sha256(producer)), (sha256(key)));
CREATE TABLE producer_entries (
    producer bytea NOT NULL,
    log bytea NOT NULL,
    sequence bigint NOT NULL,
    entry bytea NOT NULL
);
CREATE UNIQUE INDEX producer_entries_key ON producer_entries ((sha256(producer)), (sha256(log)), sequence);
CREATE TABLE producer_removals (
    producer bytea NOT NULL,
    sequence bigint NOT NULL,
    key bytea NOT NULL,
    time text COLLATE "C" NOT NULL,
    reason bytea NOT NULL
);
CREATE UNIQUE INDEX producer_removals_sequence ON producer_removals ((sha256(producer)), sequence);
CREATE INDEX producer_removals_key ON producer_removals ((sha256(producer)), (sha256(key)));

CREATE FUNCTION aavanam_inserts_only() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'the table % takes inserts only', TG_TABLE_NAME;
END $$;
CREATE TRIGGER inserts_only BEFORE UPDATE OR DELETE ON objects FOR EACH STATEMENT EXECUTE FUNCTION aavanam_inserts_only();
CREATE TRIGGER inserts_only BEFORE UPDATE OR DELETE ON edges FOR EACH STATEMENT EXECUTE FUNCTION aavanam_inserts_only();
CREATE TRIGGER inserts_only BEFORE UPDATE OR DELETE ON payloads FOR EACH STATEMENT EXECUTE FUNCTION aavanam_inserts_only();
CREATE TRIGGER inserts_only BEFORE UPDATE OR DELETE ON candidates FOR EACH STATEMENT EXECUTE FUNCTION aavanam_inserts_only();
CREATE TRIGGER inserts_only BEFORE UPDATE OR DELETE ON publications FOR EACH STATEMENT EXECUTE FUNCTION aavanam_inserts_only();
CREATE TRIGGER inserts_only BEFORE UPDATE OR DELETE ON gates FOR EACH STATEMENT EXECUTE FUNCTION aavanam_inserts_only();
CREATE TRIGGER inserts_only BEFORE UPDATE OR DELETE ON sections FOR EACH STATEMENT EXECUTE FUNCTION aavanam_inserts_only();
CREATE TRIGGER inserts_only BEFORE UPDATE OR DELETE ON producer_entries FOR EACH STATEMENT EXECUTE FUNCTION aavanam_inserts_only();
CREATE TRIGGER inserts_only BEFORE UPDATE OR DELETE ON producer_removals FOR EACH STATEMENT EXECUTE FUNCTION aavanam_inserts_only();
`, `
CREATE INDEX contexts_opened ON contexts (opened);
`, `
-- The derived tables of the read path (docs/design/backend.md, sections 2 and 4). Each one is a function
-- of objects, edges and payloads. They are not source content, so they have no inserts_only trigger.
--
-- closures: the closure of a root (KND-DEP-3), written at the first use of the root in a read context.
-- The content of a context is the closures of its roots, so the list of each context goes away. The
-- closures of the roots of each context that the store keeps give the same members (CTX-OPN-2).
CREATE TABLE closures (
    root bytea NOT NULL,
    member bytea NOT NULL,
    PRIMARY KEY (root, member)
);
INSERT INTO closures (root, member)
WITH RECURSIVE walk(root, member) AS (
    SELECT DISTINCT r.root, r.root FROM contexts x CROSS JOIN LATERAL unnest(x.roots) AS r(root)
    UNION
    SELECT w.root, e.dst FROM walk w JOIN edges e ON e.src = w.member
) SELECT root, member FROM walk;
DROP TABLE context_members;
-- text_tokens: the set of the word tokens of a text payload (CRD-TXT-3), as a bit set. It gives the
-- candidates of search_text only. A digest with no row is not in the index, and a search scans its bytes.
CREATE TABLE text_tokens (
    digest bytea PRIMARY KEY CHECK (octet_length(digest) = 32),
    bits bytea NOT NULL
);
ALTER TABLE text_tokens ALTER COLUMN bits SET STORAGE MAIN;
-- table_heads and table_rows: the declared columns and the rows of a table payload (DAT-TBL-1), written
-- at the first parse of the payload (DAT-TBL-3). A table with no row has a head and no row.
CREATE TABLE table_heads (
    digest bytea PRIMARY KEY CHECK (octet_length(digest) = 32),
    columns bytea NOT NULL,
    count integer NOT NULL
);
CREATE TABLE table_rows (
    digest bytea NOT NULL,
    idx integer NOT NULL,
    cells bytea NOT NULL,
    PRIMARY KEY (digest, idx)
);
`, `
-- The layer profile (document 23). The column objects.collection of a layer has its base collection. The
-- index finds the layers of a base collection, for the section declaration of LYR-DEF-5.
CREATE INDEX objects_layers ON objects ((sha256(collection))) WHERE kind = 'layer';
`}

// tables has each table of the schema, for the reset of the conformance driver.
var tables = []string{"objects", "edges", "payloads", "deferred", "candidates", "heads", "publications", "gates", "sections",
	"collection_locks", "contexts", "closures", "text_tokens", "table_heads", "table_rows", "producer_objects",
	"producer_entries", "producer_removals"}

// migrationLock is the key of the advisory lock that one migration run has.
const migrationLock = 0x61617661_6e616d00

// Migrate applies each schema change that the database does not have, in one transaction. Two processes
// that start at the same time do not both apply a change.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(migrationLock)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version integer PRIMARY KEY, applied timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	var applied int
	if err := tx.QueryRow(ctx, `SELECT coalesce(max(version), 0) FROM schema_migrations`).Scan(&applied); err != nil {
		return err
	}
	if applied > len(migrations) {
		return fmt.Errorf("the database has schema version %d, and this program knows %d", applied, len(migrations))
	}
	for n := applied; n < len(migrations); n++ {
		// The simple protocol sends many statements in one message.
		if _, err := tx.Exec(ctx, migrations[n], pgx.QueryExecModeSimpleProtocol); err != nil {
			return fmt.Errorf("schema change %d: %w", n+1, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, n+1); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
