// Package read has the operations that read a store through a read context (documents 08 and 09).
package read

import "github.com/mohanraj00/aavanam/backend/core/ops"

// Register adds the operations of this package to a registry.
func Register(r *ops.Registry) {
	r.Handle("open_context", OpenContext)
	r.Handle("get", Get)
}
