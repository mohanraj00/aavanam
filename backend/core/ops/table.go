package ops

// row is one row of the operation table of document 03.
type row struct {
	document string
	partial  bool // the row has the status `partial` (STS-ENV-3)
}

// table has each operation of the base profile that is not reserved (the operation table of document 03).
var table = map[string]row{
	"put":               {"02", false},
	"open_context":      {"08", false},
	"get":               {"09", false},
	"query_data":        {"09", true},
	"search":            {"09", true},
	"context_bundle":    {"09", true},
	"evidence":          {"06", true},
	"coverage":          {"07", true},
	"observations":      {"07", true},
	"execution_inputs":  {"10", false},
	"build_inputs":      {"10", false},
	"render":            {"14", true},
	"compare":           {"12", true},
	"impact":            {"12", true},
	"create_snapshot":   {"13", false},
	"create_candidate":  {"13", false},
	"publish":           {"13", false},
	"publications":      {"13", false},
	"export":            {"11", false},
	"import":            {"11", false},
	"snapshot_changes":  {"17", true},
	"item_history":      {"17", false},
	"list_items":        {"18", true},
	"read":              {"20", false},
	"search_text":       {"20", true},
	"producer_put":      {"19", false},
	"producer_get":      {"19", false},
	"producer_forget":   {"19", false},
	"producer_removals": {"19", false},
	"producer_append":   {"19", false},
	"producer_read":     {"19", false},
}

// readBinding has the operations of the network read binding, which do not write content (NET-SCP-1).
var readBinding = map[string]bool{
	"open_context": true, "get": true, "query_data": true, "search": true, "context_bundle": true, "evidence": true,
	"coverage": true, "observations": true, "render": true, "compare": true, "impact": true, "list_items": true,
	"read": true, "search_text": true, "snapshot_changes": true, "item_history": true, "publications": true, "export": true,
}

// Reserved has the profile of each reserved operation (RSV-LST-1). A store of the base profile does not
// support these operations (STS-MAL-4, RSV-RSP-1).
var Reserved = map[string]string{
	"workspace_open":             "aavanam/workspace/v1",
	"workspace_append":           "aavanam/workspace/v1",
	"workspace_read":             "aavanam/workspace/v1",
	"gc_collect":                 "aavanam/gc/v1",
	"lease_acquire":              "aavanam/lease/v1",
	"lease_release":              "aavanam/lease/v1",
	"open_cross_catalog_context": "aavanam/cross-catalog/v1",
	"query_build_inputs":         "aavanam/query-build/v1",
	"semantic_search":            "aavanam/semantic-index/v1",
	"declare_signed_collection":  "aavanam/signed-activation/v1",
	"rotate_key_set":             "aavanam/signed-activation/v1",
	"publish_signed":             "aavanam/signed-activation/v1",
	"rotate_root":                "aavanam/signed-activation/v1",
	"mark_compromise":            "aavanam/signed-activation/v1",
}

// Capabilities has each capability of the capability table and the operations that need it (CTX-PRJ-1).
var Capabilities = map[string][]string{
	"aavanam/data/v1":    {"query_data"},
	"aavanam/lexical/v1": {"search"},
	"aavanam/locks/v1":   {"execution_inputs", "build_inputs"},
	"aavanam/export/v1":  {"export"},
	"aavanam/text/v1":    {"search_text"},
}
