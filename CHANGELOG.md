# Changelog

All notable changes to the kiosk are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/). The kiosk is before
1.0, so a change to any surface can land in any release; each entry says when
one did. Run the same version on every kiosk node and the controller.

History before this file is in `git log`.

## [Unreleased]

### Breaking

The repository moved from `skeeeon/kiosk` to
[`stone-age-io/kiosk`](https://github.com/stone-age-io/kiosk), and the Go module
path is now `github.com/stone-age-io/kiosk`. GitHub redirects the old URL.

The NATS contract follows ADR 0008 in platform-docs, the ecosystem's grammar
`{app}.{scope}.{channel}.{rest}`. The scope (the node's code) is unchanged;
the channels take the shared names. Upgrade every kiosk node, the controller,
and anything else on `kiosk.>` (rule-router rules, NATS permissions) together.

| | Before | After |
|---|---|---|
| Events | `kiosk.{code}.event.…` | `kiosk.{code}.evt.…` |
| Commands | `kiosk.{code}.command.{name}` | `kiosk.{code}.cmd.{name}` |
| `KIOSK_EVENTS` subjects | `kiosk.*.event.>` | `kiosk.*.evt.>` |
| Punch time on `evt.timeclock.punch` | `occurred_at` | `ts` |
| Heartbeat, sightings | `kiosk.{code}.heartbeat`, `kiosk.{code}.sighting.raw` | unchanged |

- **Every event carries `ts`**, an RFC 3339 UTC time for when the thing
  happened. The domain fields (`completed_at`, `recorded_at`, …) stay.
- **The stream and the controller's consumers update in place.** They are
  created with CreateOrUpdate, and nats-server accepts a subject change on a
  stream that holds messages, so the controller moves them at startup. An
  event still unconsumed under the old subject at that moment is not
  projected, so upgrade while the controller is caught up, or run
  `ledger.republish` and `timeclock.republish` afterwards.
- The KV punch-state snapshot and the HTTP APIs keep `occurred_at`: they are
  not events.

### Added

- **Every event carries a `Nats-Msg-Id`.** It is stable for an event with a
  record id (`transaction.complete:<transaction_id>`, `item.<action>:<line_id>`,
  `timeclock.punch:<punch_id>`, …), so a republish inside the stream's
  duplicate window is stored once. See `docs/wire.md`.
