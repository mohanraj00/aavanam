package conformance

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mohanraj00/aavanam/backend/core/canon"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/ops"
	"github.com/mohanraj00/aavanam/backend/core/read"
	"github.com/mohanraj00/aavanam/backend/core/text"
	"github.com/mohanraj00/aavanam/backend/core/transfer"
	"github.com/mohanraj00/aavanam/backend/core/write"
)

// Registry returns a registry with each operation of the core.
func Registry() *ops.Registry {
	r := ops.NewRegistry()
	read.Register(r)
	text.Register(r)
	transfer.Register(r)
	write.Register(r)
	return r
}

// Backing is the storage of one store of a driver.
type Backing interface {
	ops.Storage
	// Fingerprint returns a digest of the content, the catalog and the producer records. Two equal stores
	// have equal fingerprints. The read contexts and the derived data have no effect on it.
	Fingerprint() string
}

// Core is a Store of the line protocol over the Go core. Each name of the protocol (`writer`, `device`,
// `output`) is one store with the role of that name.
type Core struct {
	// Open makes the empty storage of each store of the driver. Reset calls it.
	Open func() (map[string]Backing, error)
	// Debug lets a defect of an operation stop the driver, in place of `storage_error`.
	Debug bool

	stores   map[string]*ops.Store
	backings map[string]Backing
	validate Validate // the validator connection of the call in progress
	failure  error    // a failure of the validator connection in the call in progress
}

// Reset makes each store empty, with no policy, gate or section.
func (c *Core) Reset() error {
	backings, err := c.Open()
	if err != nil {
		return err
	}
	registry := Registry()
	c.backings, c.stores = backings, map[string]*ops.Store{}
	for name, backing := range backings {
		store := ops.New(ops.Role(name), backing, registry)
		store.Debug = c.Debug
		c.stores[name] = store
	}
	if device, ok := c.stores["device"]; ok {
		device.Output = c.stores["output"] // BUN-IMP-7
	}
	return nil
}

func (c *Core) store(name string) (*ops.Store, error) {
	store, ok := c.stores[name]
	if !ok {
		return nil, fmt.Errorf("this driver has no store %q", name)
	}
	return store, nil
}

// value reads one JSON value of the protocol with no repair.
func value(raw json.RawMessage) (jsontext.Value, error) {
	v, formError := jsontext.Parse(raw, jsontext.Lenient)
	if formError != nil {
		return jsontext.Value{}, fmt.Errorf("a value of the protocol is not JSON: %s", formError.Codes[0])
	}
	return v, nil
}

func name(raw json.RawMessage, what string) (string, error) {
	v, err := value(raw)
	if err != nil {
		return "", err
	}
	if v.Kind != jsontext.String {
		return "", fmt.Errorf("%s is not a string", what)
	}
	return v.Str, nil
}

// Call runs one operation on one store. The request text goes to the core with no decode.
func (c *Core) Call(storeName string, operation, request json.RawMessage, validate Validate) (json.RawMessage, error) {
	store, err := c.store(storeName)
	if err != nil {
		return nil, err
	}
	op, err := value(operation)
	if err != nil {
		return nil, err
	}
	c.validate, c.failure = validate, nil
	envelope := store.Call(context.Background(), op, request)
	if c.failure != nil {
		return nil, c.failure
	}
	return envelope.JSON(), nil
}

// Fingerprint returns the fingerprint of one store.
func (c *Core) Fingerprint(storeName string) (fingerprint string, err error) {
	backing, ok := c.backings[storeName]
	if !ok {
		return "", fmt.Errorf("this driver has no store %q", storeName)
	}
	defer func() {
		if fault, ok := recover().(*ops.Fault); ok {
			err = fault
		}
	}()
	return backing.Fingerprint(), nil
}

func text1(s string) json.RawMessage { return canon.AppendString(nil, s) }

// Accept registers a policy on each store. Its validator asks the adapter over the connection of the call.
func (c *Core) Accept(policy json.RawMessage) error {
	policyName, err := name(policy, "a policy name")
	if err != nil {
		return err
	}
	for _, store := range c.stores {
		store.Accept(policyName, func(_ context.Context, candidate, receipt, activation string) ops.Decision {
			act := json.RawMessage("null")
			if activation != "" {
				act = text1(activation)
			}
			decision, err := c.validate(policy, text1(candidate), text1(receipt), act)
			if err != nil {
				c.failure = err
				return ops.NoAnswer
			}
			switch decision {
			case Accepted:
				return ops.Accepted
			case NoAnswer:
				return ops.NoAnswer
			}
			return ops.Rejected
		})
	}
	return nil
}

// Gate declares a gated collection on each store (PUB-GAT-1).
func (c *Core) Gate(collection json.RawMessage) error {
	collectionName, err := name(collection, "a gated collection")
	if err != nil {
		return err
	}
	for _, store := range c.stores {
		if err := store.Gate(context.Background(), collectionName); err != nil {
			return err
		}
	}
	return nil
}

// Section declares the section of a collection on the writer store (PUB-SEC-1).
func (c *Core) Section(collection, section json.RawMessage) error {
	collectionName, err := name(collection, "a collection")
	if err != nil {
		return err
	}
	sectionName, err := name(section, "a section")
	if err != nil {
		return err
	}
	store, err := c.store("writer")
	if err != nil {
		return err
	}
	return store.Section(context.Background(), collectionName, sectionName)
}
