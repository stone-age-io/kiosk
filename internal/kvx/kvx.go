// Package kvx holds the kiosk's two JetStream KV buckets and the key families
// inside them.
//
// The controller writes two buckets and every node and timeclock reads them:
//
//   - KIOSK_CATALOG: items (<kiosk_code>.<item_code>), users (<user_code>) and
//     groups (<group_code>), published from the controller's database.
//   - KIOSK_STATE: punch (<user_code>), open (<user_code>) and seen
//     (<kiosk_code>.<instance_id>), projected from KIOSK_EVENTS.
//
// They were six buckets, one per family. One NATS permission block per bucket
// made the controller's user JWT larger than nats-server's default
// max_control_line (4 KB), so the server refused its connect. Two buckets
// with typed keys is the layout accessd uses for ACC_POLICY.
//
// A family is a key prefix: items.KIOSK01.HAMMER is the item HAMMER at
// KIOSK01. Family returns a Store that adds the prefix on the way in and
// strips it on the way out, so the code that owned a whole bucket keeps using
// the keys it always did. Store is deliberately narrow: an operation that
// would reach past the family, such as purging the bucket, is not on it.
package kvx

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/nats-io/nats.go/jetstream"
)

// The buckets. Both are created by the controller with History 1.
const (
	CatalogBucket = "KIOSK_CATALOG"
	StateBucket   = "KIOSK_STATE"
)

// The key families: the first token of every key.
const (
	Items  = "items"  // KIOSK_CATALOG: <kiosk_code>.<item_code>
	Users  = "users"  // KIOSK_CATALOG: <user_code>
	Groups = "groups" // KIOSK_CATALOG: <group_code>
	Punch  = "punch"  // KIOSK_STATE: <user_code>
	Open   = "open"   // KIOSK_STATE: <user_code>
	Seen   = "seen"   // KIOSK_STATE: <kiosk_code>.<instance_id>
)

// Store is one key family of a bucket. Keys are relative to the family.
type Store interface {
	// Name is "<bucket>.<family>", for logs and reports.
	Name() string
	Get(ctx context.Context, key string) (jetstream.KeyValueEntry, error)
	Put(ctx context.Context, key string, value []byte) (uint64, error)
	Delete(ctx context.Context, key string, opts ...jetstream.KVDeleteOpt) error
	// Watch watches keys, a subject filter relative to the family.
	Watch(ctx context.Context, keys string, opts ...jetstream.WatchOpt) (jetstream.KeyWatcher, error)
	// WatchAll watches the whole family, not the whole bucket.
	WatchAll(ctx context.Context, opts ...jetstream.WatchOpt) (jetstream.KeyWatcher, error)
	// ListKeys lists the family's keys, filtered on the server.
	ListKeys(ctx context.Context) (jetstream.KeyLister, error)
}

// Family returns the family of kv whose keys start with family + ".".
func Family(kv jetstream.KeyValue, family string) Store {
	return &store{kv: kv, prefix: family + "."}
}

// EnsureCatalog creates or updates the catalog bucket. Controller only.
func EnsureCatalog(ctx context.Context, js jetstream.JetStream, bucket string) (jetstream.KeyValue, error) {
	if bucket == "" {
		bucket = CatalogBucket
	}
	kv, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      bucket,
		Description: "Kiosk catalog, written by the controller and watched by managed nodes: items.<kiosk_code>.<item_code>, users.<user_code>, groups.<group_code>.",
		History:     1,
	})
	if err != nil {
		return nil, fmt.Errorf("create catalog KV %q: %w", bucket, err)
	}
	return kv, nil
}

// EnsureState creates or updates the state bucket. Controller only; the
// aggregator and the sighting ingest both call it, with the same config.
func EnsureState(ctx context.Context, js jetstream.JetStream) (jetstream.KeyValue, error) {
	kv, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      StateBucket,
		Description: "Kiosk fleet state, projected by the controller and watched by managed nodes: punch.<user_code>, open.<user_code>, seen.<kiosk_code>.<instance_id>.",
		History:     1,
	})
	if err != nil {
		return nil, fmt.Errorf("create state KV %q: %w", StateBucket, err)
	}
	return kv, nil
}

type store struct {
	kv     jetstream.KeyValue
	prefix string
}

func (s *store) Name() string { return s.kv.Bucket() + "." + strings.TrimSuffix(s.prefix, ".") }

func (s *store) Get(ctx context.Context, key string) (jetstream.KeyValueEntry, error) {
	e, err := s.kv.Get(ctx, s.prefix+key)
	if err != nil {
		return nil, err
	}
	return entry{e, s.prefix}, nil
}

func (s *store) Put(ctx context.Context, key string, value []byte) (uint64, error) {
	return s.kv.Put(ctx, s.prefix+key, value)
}

func (s *store) Delete(ctx context.Context, key string, opts ...jetstream.KVDeleteOpt) error {
	return s.kv.Delete(ctx, s.prefix+key, opts...)
}

func (s *store) Watch(ctx context.Context, keys string, opts ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	w, err := s.kv.Watch(ctx, s.prefix+keys, opts...)
	if err != nil {
		return nil, err
	}
	return newWatcher(w, s.prefix), nil
}

func (s *store) WatchAll(ctx context.Context, opts ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	return s.Watch(ctx, ">", opts...)
}

func (s *store) ListKeys(ctx context.Context) (jetstream.KeyLister, error) {
	l, err := s.kv.ListKeysFiltered(ctx, s.prefix+">")
	if err != nil {
		return nil, err
	}
	fl := &lister{l: l, out: make(chan string), done: make(chan struct{})}
	go func() {
		defer close(fl.out)
		for k := range l.Keys() {
			rel, ok := strings.CutPrefix(k, s.prefix)
			if !ok {
				continue
			}
			select {
			case fl.out <- rel:
			case <-fl.done:
				return
			}
		}
	}()
	return fl, nil
}

// entry is a KeyValueEntry whose Key is relative to its family.
type entry struct {
	jetstream.KeyValueEntry
	prefix string
}

func (e entry) Key() string { return strings.TrimPrefix(e.KeyValueEntry.Key(), e.prefix) }

// watcher forwards a family watch with relative keys. The nil entry that marks
// the end of the initial values passes through unchanged. Stop also releases
// the forwarding goroutine when nobody is reading any more.
type watcher struct {
	w    jetstream.KeyWatcher
	out  chan jetstream.KeyValueEntry
	done chan struct{}
	once sync.Once
}

func newWatcher(w jetstream.KeyWatcher, prefix string) *watcher {
	fw := &watcher{w: w, out: make(chan jetstream.KeyValueEntry), done: make(chan struct{})}
	go func() {
		defer close(fw.out)
		for e := range w.Updates() {
			var fe jetstream.KeyValueEntry
			if e != nil {
				fe = entry{e, prefix}
			}
			select {
			case fw.out <- fe:
			case <-fw.done:
				return
			}
		}
	}()
	return fw
}

func (w *watcher) Updates() <-chan jetstream.KeyValueEntry { return w.out }

func (w *watcher) Stop() error {
	w.once.Do(func() { close(w.done) })
	return w.w.Stop()
}

type lister struct {
	l    jetstream.KeyLister
	out  chan string
	done chan struct{}
	once sync.Once
}

func (l *lister) Keys() <-chan string { return l.out }

func (l *lister) Stop() error {
	l.once.Do(func() { close(l.done) })
	return l.l.Stop()
}
