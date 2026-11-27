package conformance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/mohanraj00/aavanam/backend/core/canon"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/kinds"
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

	mu       sync.RWMutex // the read binding of a driver reads the stores from a different goroutine
	stores   map[string]*ops.Store
	backings map[string]Backing
	validate Validate // the validator connection of the call in progress
	failure  error    // a failure of the validator connection in the call in progress
}

// Reset makes each store empty, with no policy, gate or section. Each store declares no profile.
func (c *Core) Reset() error { return c.open(nil) }

// Declare makes each store empty, and each store declares the profiles (LYR-PRF-1, IDX-PRF-1). A store
// declares its profiles before its first call, so the driver makes new stores.
func (c *Core) Declare(profiles []string) error {
	declared, ok := kinds.DeclaredProfiles(profiles...)
	if !ok {
		return fmt.Errorf("this driver does not have each profile of %v", profiles)
	}
	return c.open(declared)
}

func (c *Core) open(declared kinds.Profiles) error {
	backings, err := c.Open()
	if err != nil {
		return err
	}
	registry := Registry()
	stores := map[string]*ops.Store{}
	for name, backing := range backings {
		store := ops.New(ops.Role(name), backing, registry)
		store.Debug = c.Debug
		store.Profiles = declared
		stores[name] = store
	}
	if device, ok := stores["device"]; ok {
		device.Output = stores["output"] // BUN-IMP-7
	}
	c.mu.Lock()
	c.backings, c.stores = backings, stores
	c.mu.Unlock()
	return nil
}

func (c *Core) store(name string) (*ops.Store, error) {
	c.mu.RLock()
	store, ok := c.stores[name]
	c.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("this driver has no store %q", name)
	}
	return store, nil
}

// Writer returns the writer store of the driver, for its network read binding. Call Reset before it.
func (c *Core) Writer() *ops.Store {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.stores["writer"]
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

// ImportRaw runs the operation `import` on one store with a raw bundle in place of the request (BUN-RAW-4).
func (c *Core) ImportRaw(storeName string, raw []byte, validate Validate) (json.RawMessage, error) {
	store, err := c.store(storeName)
	if err != nil {
		return nil, err
	}
	c.validate, c.failure = validate, nil
	envelope := store.ImportRaw(context.Background(), bytes.NewReader(raw))
	if c.failure != nil {
		return nil, c.failure
	}
	return envelope.JSON(), nil
}

// RawBundle returns a raw form of a bundle (BUN-RAW-3). It reads no store.
func (c *Core) RawBundle(bundle json.RawMessage) ([]byte, error) {
	v, err := value(bundle)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := transfer.WriteRaw(&out, v); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
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
