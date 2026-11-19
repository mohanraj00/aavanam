package write

import (
	"bytes"

	"github.com/mohanraj00/aavanam/backend/core/canon"
	"github.com/mohanraj00/aavanam/backend/core/envelope"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/ops"
)

// The operations of the producer section (document 19). The store keeps the bytes and never parses them
// (PRD-SEC-4). Calibration data uses these objects and logs with no other check (PRD-CAL-1, PRD-CAL-2).

// The request fields of the producer section (PRD-SEC-3).
var (
	producerField = ops.Req("producer", ops.IsName(256))
	keyField      = ops.Req("key", ops.IsName(512))
	logField      = ops.Req("log", ops.IsLog)
)

// lockProducer makes the update wait for each other update of the producer, if the storage runs updates
// at the same time (PRD-OBJ-2, PRD-LOG-2, PRD-RMV-1).
func lockProducer(tx ops.Tx, producer string) {
	if lock, has := tx.(ops.ProducerLock); has {
		lock.LockProducer(producer)
	}
}

// logName returns one text for one log name (PRD-LOG-1): the canonical form of the list of its parts.
func logName(c *ops.Call) string {
	log, _ := c.Request.Get("log")
	return string(canon.Canonical(log))
}

// absent stops the operation with the `not_found` outcome of a key with no object (PRD-OBJ-3, PRD-RMV-2).
func absent(r ops.Reader, producer, key string) {
	if r.ProducerRemoved(producer, key) {
		ops.Fail("producer_removed")
	}
	ops.Fail("unknown_producer_key")
}

func removalItem(r ops.Removal) envelope.Item {
	return envelope.OK(jsontext.M("sequence", jsontext.N(float64(r.Sequence))), jsontext.M("key", jsontext.S(r.Key)),
		jsontext.M("time", jsontext.S(r.Time)), jsontext.M("reason", jsontext.S(r.Reason)))
}

// ProducerPut is the operation `producer_put` (PRD-OBJ-1, PRD-OBJ-2, PRD-OBJ-4).
func ProducerPut(c *ops.Call) envelope.Envelope {
	c.Strict([]ops.Field{producerField, keyField, ops.Req("data", ops.IsB64)}, nil, nil)
	producer, key := textOf(c.Request, "producer"), textOf(c.Request, "key")
	encoded, _ := c.Request.Get("data")
	data := ops.Unb64(encoded)
	c.S.Update(func(tx ops.Tx) {
		lockProducer(tx, producer)
		kept, has := tx.ProducerObject(producer, key)
		switch {
		case !has: // PRD-OBJ-1, PRD-OBJ-4
			tx.PutProducerObject(producer, key, data)
		case !bytes.Equal(kept, data):
			ops.Fail("producer_key_conflict") // PRD-OBJ-2
		}
	})
	return c.Envelope("ok", nil, []envelope.Item{envelope.OK(jsontext.M("key", jsontext.S(key)), jsontext.M("size", jsontext.N(float64(len(data)))))})
}

// ProducerGet is the operation `producer_get` (PRD-OBJ-3).
func ProducerGet(c *ops.Call) envelope.Envelope {
	c.Strict([]ops.Field{producerField, keyField}, nil, nil)
	producer, key := textOf(c.Request, "producer"), textOf(c.Request, "key")
	data, has := c.S.ProducerObject(producer, key)
	if !has {
		absent(c.S, producer, key)
	}
	return c.Envelope("ok", nil, []envelope.Item{envelope.OK(jsontext.M("key", jsontext.S(key)), jsontext.M("data", jsontext.S(ops.B64(data))))})
}

// ProducerForget is the operation `producer_forget`: the removal of the bytes and its record in one update
// (PRD-RMV-1 to PRD-RMV-3).
func ProducerForget(c *ops.Call) envelope.Envelope {
	c.Strict([]ops.Field{producerField, keyField, ops.Req("reason", ops.IsName(1024))}, nil, nil)
	producer, key := textOf(c.Request, "producer"), textOf(c.Request, "key")
	var record ops.Removal
	c.S.Update(func(tx ops.Tx) {
		lockProducer(tx, producer)
		if _, has := tx.ProducerObject(producer, key); !has {
			absent(tx, producer, key) // PRD-RMV-2
		}
		var count int64
		if counts, has := tx.(ops.ProducerCounts); has {
			count = counts.ProducerRemovalCount(producer)
		} else {
			count = int64(len(tx.ProducerRemovals(producer)))
		}
		record = ops.Removal{Sequence: count + 1, Key: key, Time: c.Store.Now(), Reason: textOf(c.Request, "reason")}
		tx.DeleteProducerObject(producer, key)
		tx.PutProducerRemoval(producer, record)
	})
	return c.Envelope("ok", nil, []envelope.Item{removalItem(record)})
}

// ProducerRemovals is the operation `producer_removals` (PRD-RMV-4).
func ProducerRemovals(c *ops.Call) envelope.Envelope {
	c.Strict([]ops.Field{producerField}, nil, nil)
	items := []envelope.Item{}
	for _, r := range c.S.ProducerRemovals(textOf(c.Request, "producer")) {
		items = append(items, removalItem(r))
	}
	return c.Envelope("ok", nil, items)
}

// ProducerAppend is the operation `producer_append`. The check and the write are in one update, so two
// writers cannot both add one sequence number (PRD-LOG-2).
func ProducerAppend(c *ops.Call) envelope.Envelope {
	c.Strict([]ops.Field{producerField, logField, ops.Req("sequence", ops.IsCount(1)), ops.Req("entry", ops.IsB64)}, nil, nil)
	producer, log := textOf(c.Request, "producer"), logName(c)
	number, _ := c.Request.Get("sequence")
	sequence, _ := number.Int()
	encoded, _ := c.Request.Get("entry")
	entry := ops.Unb64(encoded)
	c.S.Update(func(tx ops.Tx) {
		lockProducer(tx, producer)
		var count int64
		if counts, has := tx.(ops.ProducerCounts); has {
			count = counts.ProducerEntryCount(producer, log)
		} else {
			count = int64(len(tx.ProducerEntries(producer, log)))
		}
		if sequence != count+1 {
			ops.Fail("log_moved")
		}
		tx.PutProducerEntry(producer, log, ops.Entry{Sequence: sequence, Data: entry})
	})
	return c.Envelope("ok", nil, []envelope.Item{envelope.OK(jsontext.M("sequence", jsontext.N(float64(sequence))))})
}

// ProducerRead is the operation `producer_read` (PRD-LOG-3).
func ProducerRead(c *ops.Call) envelope.Envelope {
	c.Strict([]ops.Field{producerField, logField}, nil, nil)
	items := []envelope.Item{}
	for _, e := range c.S.ProducerEntries(textOf(c.Request, "producer"), logName(c)) {
		items = append(items, envelope.OK(jsontext.M("sequence", jsontext.N(float64(e.Sequence))), jsontext.M("entry", jsontext.S(ops.B64(e.Data)))))
	}
	return c.Envelope("ok", nil, items)
}
