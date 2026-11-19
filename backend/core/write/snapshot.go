package write

import (
	"github.com/mohanraj00/aavanam/backend/core/envelope"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/kinds"
	"github.com/mohanraj00/aavanam/backend/core/ops"
)

// CreateSnapshot is the operation `create_snapshot`. The request has the fields of a snapshot descriptor
// without `descriptor_profile` and `kind` (PUB-SNP-1). The store adds the two fields and applies STS-MAL-3
// from step 2 (PUB-SNP-2).
func CreateSnapshot(c *ops.Call) envelope.Envelope {
	given := 0
	for _, name := range []string{"descriptor_profile", "kind"} {
		if c.Request.Has(name) {
			given++
		}
	}
	if given > 0 {
		return c.Result([]envelope.Item{envelope.Refused(repeat("unknown_field", given))})
	}
	members := append(append([]jsontext.Member(nil), c.Request.Obj...),
		jsontext.M("descriptor_profile", jsontext.S(kinds.BaseProfile)), jsontext.M("kind", jsontext.S("snapshot")))
	batch := Batch{}
	out := batch.Commit(c, batch.Check(c, []jsontext.Value{jsontext.O(members...)}, false), nil)
	return c.Envelope(out.Status, out.Reasons, out.Items)
}
