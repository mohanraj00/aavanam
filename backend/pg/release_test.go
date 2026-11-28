package pg

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// loadDump replaces the content of the test database with the statements of a dump. It removes the schema
// `public` with each table first, and it runs the dump on a connection of its own.
func loadDump(ctx context.Context, dsn, dump string) error {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return err
	}
	defer conn.Close(context.WithoutCancel(ctx))
	if _, err := conn.Exec(ctx, `DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public`, pgx.QueryExecModeSimpleProtocol); err != nil {
		return err
	}
	_, err = conn.Exec(ctx, dump, pgx.QueryExecModeSimpleProtocol)
	return err
}

// MIG-VER-1, MIG-VER-2, MIG-STG-1 and MIG-STG-3: the dump of fixtures/releases/0.6.0/backend is a database
// that release 0.6.0 wrote (fixtures/README.md, "Release stores"). This version opens it and keeps its
// content and its read contexts. A second open applies no schema change again. A database with a schema
// version that this program does not know does not open, and it does not change.
func TestOpenOfTheRelease060Database(t *testing.T) {
	dsn := os.Getenv("AAVANAM_PG_DSN")
	if dsn == "" {
		t.Skip("AAVANAM_PG_DSN is not set")
	}
	release := filepath.Join("..", "..", "fixtures", "releases", "0.6.0")
	data, err := os.ReadFile(filepath.Join(release, "backend", "postgresql.sql"))
	if err != nil {
		t.Fatal(err)
	}
	dump := string(data)
	var contexts map[string]map[string]string
	if data, err = os.ReadFile(filepath.Join(release, "backend", "contexts.json")); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &contexts); err != nil || len(contexts["writer"]) == 0 {
		t.Fatalf("the release store has no context of the writer store: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	// The other tests of this package open the database after this test, so it gets an empty schema again.
	t.Cleanup(func() {
		if err := loadDump(context.Background(), dsn, `SELECT 1`); err != nil {
			t.Error(err)
		}
	})
	if err := loadDump(ctx, dsn, dump); err != nil {
		t.Fatal(err)
	}
	objects := strings.Count(dump, "\nINSERT INTO public.objects ")
	for n := 0; n < 2; n++ { // the second open finds each schema change in the database
		s, err := Open(ctx, dsn)
		if err != nil {
			t.Fatalf("open %d of the database of release 0.6.0: %v", n+1, err)
		}
		if got := count(t, s, `SELECT max(version) FROM schema_migrations`); got != len(migrations) {
			t.Errorf("open %d: schema version %d, want %d", n+1, got, len(migrations))
		}
		if got := count(t, s, `SELECT count(*) FROM schema_migrations`); got != len(migrations) {
			t.Errorf("open %d: %d schema change records, want %d", n+1, got, len(migrations))
		}
		if got := count(t, s, `SELECT count(*) FROM objects`); got != objects || objects == 0 {
			t.Errorf("open %d: %d objects, want the %d objects of the dump", n+1, got, objects)
		}
		r := s.Session(ctx)
		if records := r.Removals(); len(records) != 1 || records[0].Sequence != 1 {
			t.Errorf("open %d: the removal records are %+v, want the one record of the release", n+1, records)
		}
		for name, id := range contexts["writer"] {
			if record, ok := r.Context(id); !ok || len(record.Roots) != 1 || len(r.Members(id)) == 0 {
				t.Errorf("open %d: the read context %s of the release: %+v, %t", n+1, name, record, ok)
			}
		}
		s.Close()
	}
	later := strconv.Itoa(len(migrations) + 1)
	if err := loadDump(ctx, dsn, dump+"\nINSERT INTO public.schema_migrations (version) VALUES ("+later+");\n"); err != nil {
		t.Fatal(err)
	}
	if s, err := Open(ctx, dsn); err == nil {
		s.Close()
		t.Error("a database with a schema version that this program does not know opened (MIG-STG-3)")
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	var version, kept int
	if err := conn.QueryRow(ctx, `SELECT (SELECT max(version) FROM schema_migrations), (SELECT count(*) FROM objects)`).Scan(&version, &kept); err != nil {
		t.Fatal(err)
	}
	if strconv.Itoa(version) != later || kept != objects {
		t.Errorf("the refused open changed the database: schema version %d, %d objects", version, kept)
	}
}
