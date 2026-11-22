package events

import (
	"encoding/json"
	"testing"
	"time"

	kitsubjects "github.com/stone-age-io/kit/subjects"
)

// recordingPublisher captures every PublishBytes call so the test can
// inspect what would have hit NATS. The captured Data is the
// already-marshaled JSON bytes events.Publish produced — tests that need
// to assert on payload contents can json.Unmarshal them.
type recordingPublisher struct {
	calls []struct {
		Subject string
		Data    []byte
	}
}

func (r *recordingPublisher) PublishBytes(subject string, data []byte) error {
	// Copy the slice so tests inspecting r.calls later aren't affected if
	// the production caller reuses its buffer.
	cp := append([]byte(nil), data...)
	r.calls = append(r.calls, struct {
		Subject string
		Data    []byte
	}{subject, cp})
	return nil
}

func (r *recordingPublisher) Close() {}

func TestPublish_NoPublisher_DoesNotPanic(t *testing.T) {
	// Reset just in case some other test left a publisher set.
	SetPublisher(nil)
	// Should run cleanly — only slog logs, no NATS publish.
	Publish("kiosk.TEST.evt.item.checkout", map[string]any{"x": 1})
}

func TestPublish_WithPublisher_InvokesIt(t *testing.T) {
	rp := &recordingPublisher{}
	SetPublisher(rp)
	defer SetPublisher(nil)

	Publish("kiosk.TEST.evt.transaction.complete", map[string]any{"lines": 3})

	if len(rp.calls) != 1 {
		t.Fatalf("publisher calls: want 1, got %d", len(rp.calls))
	}
	if rp.calls[0].Subject != "kiosk.TEST.evt.transaction.complete" {
		t.Errorf("subject: got %q", rp.calls[0].Subject)
	}
}

func TestSetPublisher_NilDeactivates(t *testing.T) {
	rp := &recordingPublisher{}
	SetPublisher(rp)
	SetPublisher(nil)

	Publish("kiosk.TEST.evt.item.consume", map[string]any{})
	if len(rp.calls) != 0 {
		t.Errorf("after SetPublisher(nil) publisher should not be invoked, got %d calls", len(rp.calls))
	}
}

// msgIDPublisher records the Nats-Msg-Id the production publisher would set.
type msgIDPublisher struct {
	subjects, ids []string
	payloads      [][]byte
}

func (p *msgIDPublisher) PublishBytes(string, []byte) error { return nil }
func (p *msgIDPublisher) Close()                            {}
func (p *msgIDPublisher) PublishEvent(subject, id string, data []byte) error {
	p.subjects, p.ids, p.payloads = append(p.subjects, subject), append(p.ids, id), append(p.payloads, data)
	return nil
}

// Every event carries ts, and a Nats-Msg-Id that is stable for an event with a
// record id (a republish is stored once) and fresh for one without.
func TestPublishSetsTSAndMsgID(t *testing.T) {
	p := &msgIDPublisher{}
	SetPublisher(p)
	t.Cleanup(func() { SetPublisher(nil) })

	done := time.Date(2026, 5, 1, 12, 5, 0, 0, time.UTC)
	tx := BuildTransactionCompletePayload(TransactionCompleteInput{TransactionID: "tx-1", CompletedAt: done})
	Publish(TransactionCompleteSubject("KIOSK-A"), tx)
	Publish(TransactionCompleteSubject("KIOSK-A"), tx) // a republish
	Publish(ItemActionSubject("KIOSK-A", "checkout"), map[string]any{"line_id": "ln-1"})
	Publish(LowStockAlertSubject("KIOSK-A"), map[string]any{"item_code": "GLOVE"})
	Publish(LowStockAlertSubject("KIOSK-A"), map[string]any{"item_code": "GLOVE"})

	if p.ids[0] != "transaction.complete:tx-1" || p.ids[1] != p.ids[0] {
		t.Errorf("transaction ids = %q, %q, want both transaction.complete:tx-1", p.ids[0], p.ids[1])
	}
	if p.ids[2] != "item.checkout:ln-1" {
		t.Errorf("item id = %q, want item.checkout:ln-1", p.ids[2])
	}
	if p.ids[3] == "" || p.ids[3] == p.ids[4] {
		t.Errorf("alert ids = %q, %q, want two different fresh ids", p.ids[3], p.ids[4])
	}

	var first, alert struct {
		TS string `json:"ts"`
	}
	_ = json.Unmarshal(p.payloads[0], &first)
	if first.TS != "2026-05-01T12:05:00Z" {
		t.Errorf("transaction ts = %q, want its completed_at", first.TS)
	}
	_ = json.Unmarshal(p.payloads[3], &alert)
	if _, err := time.Parse(time.RFC3339, alert.TS); err != nil {
		t.Errorf("alert ts = %q, want the publish time in RFC 3339", alert.TS)
	}
}

// The stream subject and the controller's consumer filters, through the kit's
// overlap check (ADR 0008): every filter is inside the stream, and commands,
// heartbeats and sightings are outside it.
func TestStreamCapturesOnlyEvents(t *testing.T) {
	stream := StreamSubjectFilter()
	for _, f := range []string{TransactionCompleteFilter(), ItemActionFilter(), InventoryAdjustFilter(),
		IntegrityRebuildFilter(), AdminCloseFilter(), InstanceLifecycleFilter(), ReceiptTransactionFilter(),
		LowStockAlertFilter(), MaintenanceAlertFilter(), TimeclockPunchFilter(), ScanRFIDObservedFilter()} {
		if !kitsubjects.Overlap(stream, f) {
			t.Errorf("consumer filter %s is outside the stream subject %s", f, stream)
		}
	}
	for _, f := range []string{CommandSubscribePattern("K1"), HeartbeatFilter(), SightingFilter()} {
		if kitsubjects.Overlap(stream, f) {
			t.Errorf("%s is not an event but the stream subject %s captures it", f, stream)
		}
	}
}
