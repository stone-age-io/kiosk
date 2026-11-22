package kvx

import (
	"context"
	"sort"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func jsForTest(t *testing.T) jetstream.JetStream {
	t.Helper()
	ns, err := natsserver.NewServer(&natsserver.Options{
		Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: t.TempDir(), NoSigs: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	go ns.Start()
	if !ns.ReadyForConnections(5 * time.Second) {
		t.Fatal("nats-server not ready")
	}
	t.Cleanup(ns.Shutdown)
	nc, err := nats.Connect(ns.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	return js
}

// The families of one bucket never see each other's keys: not in Get, not in
// a watch, and not in ListKeys, which the catalog integrity check uses to
// delete what the database does not have.
func TestFamiliesShareABucketWithoutSeeingEachOther(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	kv, err := EnsureCatalog(ctx, jsForTest(t), "")
	if err != nil {
		t.Fatal(err)
	}
	items, users := Family(kv, Items), Family(kv, Users)

	for _, k := range []string{"K1.HAMMER", "K1.SAW", "K2.DRILL"} {
		if _, err := items.Put(ctx, k, []byte(k)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := users.Put(ctx, "U1", []byte("u1")); err != nil {
		t.Fatal(err)
	}

	// The keys land under their family in the bucket.
	if _, err := kv.Get(ctx, "items.K1.HAMMER"); err != nil {
		t.Fatalf("raw key: %v", err)
	}

	// Get is relative.
	e, err := items.Get(ctx, "K1.SAW")
	if err != nil || e.Key() != "K1.SAW" || string(e.Value()) != "K1.SAW" {
		t.Fatalf("Get = %v, %v", e, err)
	}
	if _, err := users.Get(ctx, "K1.SAW"); err == nil {
		t.Fatal("users sees an item")
	}

	// ListKeys stays in the family.
	l, err := items.ListKeys(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for k := range l.Keys() {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if want := []string{"K1.HAMMER", "K1.SAW", "K2.DRILL"}; !equal(keys, want) {
		t.Fatalf("items keys = %v, want %v", keys, want)
	}

	// A filtered watch: this kiosk's slice, relative keys, then the nil
	// marker, then a live delete.
	w, err := items.Watch(ctx, "K1.>")
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()
	var got []string
	for e := range w.Updates() {
		if e == nil {
			break
		}
		got = append(got, e.Key())
	}
	sort.Strings(got)
	if want := []string{"K1.HAMMER", "K1.SAW"}; !equal(got, want) {
		t.Fatalf("watch initial = %v, want %v", got, want)
	}
	if err := items.Delete(ctx, "K1.SAW"); err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-w.Updates():
		if e == nil || e.Key() != "K1.SAW" || e.Operation() != jetstream.KeyValueDelete {
			t.Fatalf("delete update = %v", e)
		}
	case <-ctx.Done():
		t.Fatal("no delete update")
	}

	// WatchAll is the family, not the bucket.
	wa, err := users.WatchAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer wa.Stop()
	var all []string
	for e := range wa.Updates() {
		if e == nil {
			break
		}
		all = append(all, e.Key())
	}
	if want := []string{"U1"}; !equal(all, want) {
		t.Fatalf("users WatchAll = %v, want %v", all, want)
	}
}

// Stop releases a watch nobody reads any more.
func TestStopWithoutReading(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	kv, err := EnsureState(ctx, jsForTest(t))
	if err != nil {
		t.Fatal(err)
	}
	punch := Family(kv, Punch)
	for _, k := range []string{"U1", "U2", "U3"} {
		if _, err := punch.Put(ctx, k, []byte("in")); err != nil {
			t.Fatal(err)
		}
	}
	w, err := punch.WatchAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Stop(); err != nil {
		t.Fatal(err)
	}
	for range w.Updates() { // must close, not block
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
