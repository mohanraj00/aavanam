package read

import (
	"github.com/mohanraj00/aavanam/backend/core/envelope"
	"github.com/mohanraj00/aavanam/backend/core/ops"
)

// Get is the operation `get`: it tells for each exact reference if the object is in the read context
// (QRY-GET-1, QRY-GET-2).
func Get(c *ops.Call) envelope.Envelope {
	c.Form("field_type", ops.Req("context", ops.IsStr), ops.Req("refs", ops.IsRefs))
	context := c.Context()
	refs, _ := c.Request.Get("refs")
	items := make([]envelope.Item, len(refs.Arr))
	for n, ref := range refs.Arr {
		if code := context.Locate(ref.Str); code != "" {
			items[n] = envelope.Refused([]string{code}, ref.Str)
		} else {
			items[n] = envelope.Ref(ref.Str)
		}
	}
	return c.Result(items).WithContext(context.ID)
}
