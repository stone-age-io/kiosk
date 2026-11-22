package timeclock

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/stone-age-io/kiosk/internal/kvx"
)

// CheckoutWatcher hydrates a CheckoutFleet from the controller-written
// open family of the state bucket. Near-verbatim sibling of Watcher (which feeds
// the punch Fleet) — the only differences are the bucket, the payload
// type, and latest-wins Upsert. WatchAll replays the full bucket on start, so
// a process restart recovers fleet state without local persistence.
type CheckoutWatcher struct {
	js     jetstream.JetStream
	fleet  *CheckoutFleet
	cancel context.CancelFunc
	kw     jetstream.KeyWatcher
}

// NewCheckoutWatcher wires a watcher onto an existing CheckoutFleet. Call Start
// to attach.
func NewCheckoutWatcher(js jetstream.JetStream, fleet *CheckoutFleet) *CheckoutWatcher {
	return &CheckoutWatcher{js: js, fleet: fleet}
}

// Start opens the bucket and begins projecting entries into the CheckoutFleet.
// Errors are startup-level (bucket missing — the controller provisions it, so a
// kiosk booting before the controller's first run will fail here; callers log
// and continue, degraded to local-only open-checkout state).
func (w *CheckoutWatcher) Start(parent context.Context) error {
	ctx, cancel := context.WithCancel(parent)
	w.cancel = cancel

	kv, err := w.js.KeyValue(ctx, kvx.StateBucket)
	if err != nil {
		cancel()
		return fmt.Errorf("open state KV %q: %w", kvx.StateBucket, err)
	}
	kw, err := kvx.Family(kv, kvx.Open).WatchAll(ctx)
	if err != nil {
		cancel()
		return fmt.Errorf("watch %s.%s: %w", kvx.StateBucket, kvx.Open, err)
	}
	w.kw = kw

	go w.run(ctx, kw)
	slog.Info("kiosk.timeclock.checkout_watcher.started", "bucket", kvx.StateBucket, "family", kvx.Open)
	return nil
}

// Stop tears down the watcher. Safe to call multiple times.
func (w *CheckoutWatcher) Stop() {
	if w.kw != nil {
		w.kw.Stop()
		w.kw = nil
	}
	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
	}
}

func (w *CheckoutWatcher) run(ctx context.Context, kw jetstream.KeyWatcher) {
	for {
		select {
		case <-ctx.Done():
			return
		case entry, ok := <-kw.Updates():
			if !ok {
				return
			}
			if entry == nil {
				slog.Info("kiosk.timeclock.open_checkouts_state.snapshot_done")
				continue
			}
			w.apply(entry)
		}
	}
}

func (w *CheckoutWatcher) apply(entry jetstream.KeyValueEntry) {
	switch entry.Operation() {
	case jetstream.KeyValueDelete, jetstream.KeyValuePurge:
		w.fleet.Delete(entry.Key())
		return
	}
	var p OpenCheckoutsStatePayload
	if err := json.Unmarshal(entry.Value(), &p); err != nil {
		slog.Warn("kiosk.timeclock.open_checkouts_state.bad_payload",
			"key", entry.Key(), "error", err)
		return
	}
	if p.UserCode == "" {
		p.UserCode = entry.Key()
	}
	// Latest-wins: a present-but-empty Rows slice is the worker's "returned
	// everything" state and must overwrite a prior non-empty one.
	w.fleet.Upsert(p)
}
