// Package text has the deterministic text search of document 20 and the tokens of its index.
package text

import "github.com/mohanraj00/aavanam/backend/core/ops"

// Register adds the operations of this package and the indexer of the text tokens to a registry.
func Register(r *ops.Registry) {
	r.Handle("search_text", SearchText)
	r.OnBytes(Index)
}
