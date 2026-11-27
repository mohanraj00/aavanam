package write

import (
	"github.com/mohanraj00/aavanam/backend/core/envelope"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/ops"
)

// Register adds the operations of this package to a registry.
func Register(r *ops.Registry) {
	r.Handle("put", Put)
	r.Handle("create_snapshot", CreateSnapshot)
	r.Handle("create_candidate", CreateCandidate)
	r.Handle("publish", Publish)
	r.Handle("publications", Publications)
	r.Handle("remove", Remove)
	r.Handle("removals", Removals)
	r.Handle("gc_collect", Collect)
	r.Handle("producer_put", ProducerPut)
	r.Handle("producer_get", ProducerGet)
	r.Handle("producer_forget", ProducerForget)
	r.Handle("producer_removals", ProducerRemovals)
	r.Handle("producer_append", ProducerAppend)
	r.Handle("producer_read", ProducerRead)
}

// Put is the operation `put`: it checks each descriptor text in the steps of STS-MAL-3 and writes all of
// them or none (STS-MAL-2). The payload bytes of the request are base64 text.
func Put(c *ops.Call) envelope.Envelope {
	texts, _ := c.Request.Get("descriptors")
	encoded, hasPayloads := c.Request.Get("payloads")
	c.Form("field_type", ops.Req("descriptors", ops.ListOf(ops.IsStr, false)),
		ops.Opt("payloads", func(v jsontext.Value) bool { return v.Kind == jsontext.Object }))
	batch := Batch{Data: map[string][]byte{}}
	if hasPayloads {
		for _, m := range encoded.Obj {
			batch.Data[m.Key] = ops.Unb64(m.Val)
		}
	}
	out := batch.Commit(c, batch.Check(c, texts.Arr, true), nil)
	return c.Envelope(out.Status, out.Reasons, out.Items)
}
