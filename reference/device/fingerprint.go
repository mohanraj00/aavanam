package device

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
)

// Fingerprint returns a digest of the content of the file of the store: the objects, the bytes and the
// deferred checks. The read contexts, the import records and the derived tables have no effect on it.
// If the store cannot read the file, the text tells the error, and it is not a digest.
func (s *Store) Fingerprint() string {
	var lines []string
	queries := []struct{ table, query string }{
		{"objects", `SELECT ref, kind, canonical FROM main.objects`},
		{"payloads", `SELECT digest, size, data FROM main.payloads`},
		{"deferred", `SELECT digest, ref, x'' FROM main.deferred`},
	}
	for _, q := range queries {
		rows, err := s.db.Query(q.query)
		if err != nil {
			return "error: " + err.Error()
		}
		for rows.Next() {
			var a, b, c []byte
			if err := rows.Scan(&a, &b, &c); err != nil {
				rows.Close()
				return "error: " + err.Error()
			}
			sum := sha256.Sum256(c)
			lines = append(lines, fmt.Sprintf("%s:%x:%x:%x", q.table, a, b, sum))
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return "error: " + err.Error()
		}
	}
	sort.Strings(lines)
	h := sha256.New()
	for _, line := range lines {
		h.Write([]byte(line))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}
