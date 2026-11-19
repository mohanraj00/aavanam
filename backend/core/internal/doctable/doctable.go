// Package doctable reads a table of a contract document. The tests of the core use it to compare the
// tables in the code with the tables of the specification.
package doctable

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// root returns the folder of the repository.
func root() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..")
}

// Spec returns the text of SPEC.md.
func Spec() (string, error) {
	data, err := os.ReadFile(filepath.Join(root(), "SPEC.md"))
	return string(data), err
}

// Text returns the text of the contract document with the given number, for example "03".
func Text(document string) (string, error) {
	paths, err := filepath.Glob(filepath.Join(root(), "docs", "contracts", document+"-*.md"))
	if err != nil || len(paths) != 1 {
		return "", fmt.Errorf("document %s: found %d files: %v", document, len(paths), err)
	}
	data, err := os.ReadFile(paths[0])
	return string(data), err
}

// Clause returns the text of one clause of a contract document, from its ID to the end of its paragraph.
func Clause(document, id string) (string, error) {
	text, err := Text(document)
	if err != nil {
		return "", err
	}
	_, after, found := strings.Cut(text, "**"+id+".**")
	if !found {
		return "", fmt.Errorf("document %s has no clause %s", document, id)
	}
	clause, _, _ := strings.Cut(after, "\n\n")
	return clause, nil
}

// Rows returns the cells of each body row of the table that follows a marker in the contract document with
// the given number, for example "03". If the marker is the header row of the table, the body starts after it.
func Rows(document, marker string) ([][]string, error) {
	data, err := Text(document)
	if err != nil {
		return nil, err
	}
	_, after, found := strings.Cut(data, marker)
	if !found {
		return nil, fmt.Errorf("document %s has no text %q", document, marker)
	}
	var rows [][]string
	if strings.HasPrefix(marker, "|") {
		rows = append(rows, nil) // the marker is the header row
	}
	for _, line := range strings.Split(after, "\n") {
		if !strings.HasPrefix(line, "|") {
			if len(rows) > 1 {
				break
			}
			continue // the rest of the marker line, and the empty line before the table
		}
		var row []string
		for _, cell := range strings.Split(strings.Trim(line, "|"), "|") {
			row = append(row, strings.TrimSpace(cell))
		}
		rows = append(rows, row)
	}
	if len(rows) < 3 {
		return nil, fmt.Errorf("the table after %q in document %s has no rows", marker, document)
	}
	return rows[2:], nil // without the header and the separator
}

// Names returns the names of a cell: each part between commas, with no backticks. A cell `-` has no names.
func Names(cell string) []string {
	var out []string
	for _, part := range strings.Split(cell, ",") {
		if name := strings.Trim(strings.TrimSpace(part), "`"); name != "" && name != "-" {
			out = append(out, name)
		}
	}
	return out
}
