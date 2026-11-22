// Package events is the single integration point for the kiosk's emitted
// events. v1 wrote a structured log line per event; v2 adds an optional
// NATS publish using subjects that already followed NATS hierarchy.
//
// The publisher is global because every commit path calls Publish without
// threading a handle. Tests inject a fake via SetPublisher.
package events

import (
	"encoding/json"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/nats-io/nuid"
)

// publisher holds the active Publisher (or nil when disabled). Atomic so
// tests can swap it without locking; production sets it once at startup.
var publisher atomic.Pointer[Publisher]

// SetPublisher installs the global publisher. Pass nil (or call with no
// publisher set) to keep events slog-only.
func SetPublisher(p Publisher) {
	if p == nil {
		publisher.Store(nil)
		return
	}
	publisher.Store(&p)
}

// CurrentPublisher returns the active publisher or nil. Mainly for tests
// and shutdown wiring.
func CurrentPublisher() Publisher {
	p := publisher.Load()
	if p == nil {
		return nil
	}
	return *p
}

// eventPublisher is a Publisher that can set a Nats-Msg-Id. The production
// publisher implements it; a test fake that only implements PublishBytes
// still works, without the header.
type eventPublisher interface {
	PublishEvent(subject, msgID string, data []byte) error
}

// Publish emits an event. Always logs via slog; if a publisher is set,
// also publishes JSON-encoded payload to the subject. The signature is
// intentionally narrow (no error return) — callers (commit hook,
// post-DB-transaction) cannot meaningfully recover, and we don't want to
// bake retry logic into write paths. Publish failures land in slog at
// warn level.
//
// Every event follows ADR 0008 rule 3 (platform-docs):
//
//   - It carries "ts", an RFC 3339 UTC time for when the thing happened. The
//     payload builders set it from the event's own time (a transaction's
//     completed_at, a punch's time), so a republished event keeps its
//     original ts. A payload without one gets the publish time.
//   - It carries a Nats-Msg-Id, from MsgID, so the stream stores it once
//     within its duplicate window.
//
// The payload is marshaled once and the resulting bytes flow to both the
// slog line (as a json.RawMessage so the log encoder inlines it instead of
// re-quoting) and the publisher.
func Publish(subject string, payload any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		slog.Warn("kiosk.event.marshal_failed", "subject", subject, "error", err)
		return
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		slog.Warn("kiosk.event.not_an_object", "subject", subject, "error", err)
		return
	}
	if _, ok := fields["ts"]; !ok {
		fields["ts"], _ = json.Marshal(TS(time.Now()))
		if raw, err = json.Marshal(fields); err != nil {
			slog.Warn("kiosk.event.marshal_failed", "subject", subject, "error", err)
			return
		}
	}
	slog.Info("kiosk.event", "subject", subject, "payload", json.RawMessage(raw))

	p := CurrentPublisher()
	if p == nil {
		return
	}
	if ep, ok := p.(eventPublisher); ok {
		err = ep.PublishEvent(subject, MsgID(subject, fields), raw)
	} else {
		err = p.PublishBytes(subject, raw)
	}
	if err != nil {
		slog.Warn("kiosk.event.publish_failed", "subject", subject, "error", err)
	}
}

// TS formats t as an event's "ts": RFC 3339 in UTC.
func TS(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

// stableIDField names, for each event kind, the payload field that identifies
// one event. Its value makes the Nats-Msg-Id, so a ledger.republish or
// timeclock.republish of the same event inside the stream's duplicate window
// is stored once. A kind listed here must carry a field that is unique per
// event: a transaction has one transaction.complete, and a line one item.*.
var stableIDField = map[string]string{
	"transaction.complete": "transaction_id",
	"checkout.admin_close": "line_id",
	"inventory.adjust":     "adjustment_id",
	"timeclock.punch":      "punch_id",
	"instance.lifecycle":   "source_audit_id",
}

// MsgID is the Nats-Msg-Id for an event on subject with the given payload
// fields: "<kind>:<id>" from the kind's stable field (item.* events use
// line_id), or a fresh unique id for an event that has none, such as an alert.
func MsgID(subject string, fields map[string]json.RawMessage) string {
	parts := strings.SplitN(subject, ".", 4)
	if len(parts) < 4 {
		return nuid.Next()
	}
	kind := parts[3]
	field, ok := stableIDField[kind]
	if !ok && strings.HasPrefix(kind, "item.") {
		field, ok = "line_id", true
	}
	if ok {
		var id string
		if json.Unmarshal(fields[field], &id) == nil && id != "" {
			return kind + ":" + id
		}
	}
	return nuid.Next()
}
