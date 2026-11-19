// Package transfer has the operations of the base profile for bundles and input locks: `export` and
// `import` (document 11), and `execution_inputs` and `build_inputs` (document 10).
package transfer

import "github.com/mohanraj00/aavanam/backend/core/ops"

// Register adds the operations of this package to a registry.
func Register(r *ops.Registry) {
	r.Handle("export", Export)
	r.Handle("import", Import)
	r.Handle("execution_inputs", ExecutionInputs)
	r.Handle("build_inputs", BuildInputs)
}
