// Package read has the operations that read a store through a read context (documents 06 to 09, 12, 14,
// 17, 18 and 20), and the store reads of document 17.
package read

import "github.com/mohanraj00/aavanam/backend/core/ops"

// Register adds the operations of this package to a registry.
func Register(r *ops.Registry) {
	r.Handle("open_context", OpenContext)
	r.Handle("get", Get)
	r.Handle("read", Read)
	r.Handle("evidence", Evidence)
	r.Handle("observations", Observations)
	r.Handle("compare", Compare)
	r.Handle("query_data", QueryData)
	r.Handle("search", Search)
	r.Handle("context_bundle", ContextBundle)
	r.Handle("coverage", Coverage)
	r.Handle("impact", Impact)
	r.Handle("render", Render)
	r.Handle("snapshot_changes", SnapshotChanges)
	r.Handle("item_history", ItemHistory)
	r.Handle("list_items", ListItems)
}
