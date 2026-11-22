# Kiosk Documentation

This folder holds the topic pages for the kiosk node (`kiosk-app`), the
controller (`kiosk-controller`) and the timeclock terminal
(`kiosk-timeclock`). The repository's [README](../README.md) lists the
features. Read [Overview](overview.md) first.

---

## Start Here

- **[Overview](overview.md)**: custody and location, the mental model (kiosk
  node, terminal, reader, enclosure, item, instance), how the three binaries
  fit together and how a deployment grows.

---

## Setup and Operation

- **[Configuration](configuration.md)**: `kiosk.yaml`, `controller.yaml` and
  `timeclock.yaml`, environment overrides, the timeclock and its interlocks,
  the three modes of the timeclock terminal, branding and custom CSS.
- **[Development](development.md)**: prerequisites, the backend and frontend
  dev loops, database reset and the Go test suite.
- **[Operations](operations.md)**: deployment, backups, ledger integrity,
  resync to the controller, NATS failure modes, stock adjustments, serialized
  instances, the timeclock terminal (deployment, SSO and SMTP, hardening) and
  password reset.
- **[Troubleshooting](troubleshooting.md)**: common errors and what they mean.

---

## Reference

- **[API Reference](api.md)**: the custom `/api/kiosk/*`, `/api/controller/*`
  and `/api/self/timeclock/*` endpoints, and the PocketBase collection rules.
- **[Wire Protocol](protocol.md)**: every NATS subject (events, commands,
  heartbeats) with its payload and reply shapes. Read it to connect an
  external system to the kiosk over NATS.
- **[Schema](schema.md)**: the collections (the `time_punches` ledger
  included), controller-only fields, the worker-auth migration of the
  timeclock terminal, the `open_checkouts` cardinality rules and the CSV
  import format.
- **[Ledger](ledger.md)**: the append-only design, the single write path,
  `open_checkouts` as a derived view, the controller's projected ledger and
  drift recovery.

---

## Subsystems

- **[Central Controller](controller.md)**: deployments with more than one
  kiosk node. Catalog distribution, transaction aggregation, heartbeats and
  online status, remote inventory commands and NATS provisioning.
- **[Notifications](notifications.md)**: receipts, low-stock alerts and
  scheduled digests, in standalone and managed mode.
- **[RFID](rfid.md)**: the Impinj R700 and LLRP integration, its two modes
  (`counter_scan` and `enclosure_diff`), the SSE cart-events channel and the
  `cart.start` and `read.trigger` commands. It also covers USB HID badge
  readers.

---

## Project Status

- **[Shipped & Roadmap](roadmap.md)**: what the binaries do now and what is
  deferred on purpose.

---

## Plans

These are design documents for work in progress. Each has its own status
line. None of them describes what the binaries do today.

- **[Asset-Tracker Generalization](asset-tracker-plan.md)**: custody and
  coarse location on a jobsite, many readers and enclosures per kiosk node,
  and the "smart cache" form factor.
- **[Location & Sightings](location-sightings-plan.md)**: the advisory
  asset-location layer, external gateways and `last_observed_*`.
- **[Northwind Demo](demo-plan.md)**: a demo estate with three kiosk nodes, a
  timeclock terminal and a controller. The fixture and both `demo-seed`
  subcommands are done. To start one kiosk node alone, run
  `KIOSK_CONFIG=demo/standalone.yaml ./kiosk-app demo-seed --confirm`. The
  mock RFID reader and the rule-router rules that send live traffic through
  the real commit path are still to come.
