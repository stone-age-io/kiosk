# Northwind kiosk demo: live activity

rule-router rules that keep the Northwind estate busy: workers walking up to
three kiosks, scanning one thing and confirming, and punching in and out. A
mock RFID reader lets a presenter take a tool out of a cabinet by deleting a
line from a text file. The estate is the same company, sites and people as the
platform's and access control's demos. `docs/demo-plan.md` explains the shape.

**This is demo tooling.** Nothing here ships to an install, and the passwords
are well-known.

> **This driver is a scheduled, unauthenticated write path into a kiosk.**
> `/api/kiosk/*` takes no login, because the kiosk binds `127.0.0.1` and the box
> is the trust boundary. So rule-router must run on the kiosks' host, and this
> must never become a deployment pattern. access control's driver publishes on
> an account-scoped, credentialed NATS subject instead; this one cannot.

## Run the fleet

Everything on one host, one NATS server with JetStream, from the repository
root. Each node gets its own data directory.

```bash
# 0. NATS with JetStream (or the platform's embedded server)
nats-server -js

# 1. Controller schema and catalogue (it creates the stream and buckets itself)
KIOSK_CONFIG=demo/controller.yaml ./kiosk-controller demo-seed --confirm

# 2. The three kiosks, managed: the catalogue arrives over KV
KIOSK_CONFIG=demo/kc-dc1-crib.yaml  ./kiosk-app serve --dir pb_data_kc-dc1-crib
KIOSK_CONFIG=demo/kc-dc1-dock.yaml  ./kiosk-app serve --dir pb_data_kc-dc1-dock
KIOSK_CONFIG=demo/sgf-xd2-crib.yaml ./kiosk-app serve --dir pb_data_sgf-xd2-crib

# 3. Quantities, thresholds and serialized units, over the command bus
KIOSK_CONFIG=demo/controller.yaml ./kiosk-controller demo-seed --remote --confirm

# 4. The controller and the timeclock terminal
KIOSK_CONFIG=demo/controller.yaml ./kiosk-controller serve
KIOSK_CONFIG=demo/timeclock.yaml  ./kiosk-timeclock serve

# 5. Warm up for ~10 minutes, then go ambient. Same rules, one variable.
DEMO_CRON="*/3 * * * * *"  rule-router --config demo/rule-router.yaml --rules demo/rules
DEMO_CRON="*/45 * * * * *" rule-router --config demo/rule-router.yaml --rules demo/rules
```

`DEMO_CRON` has six fields, so `*/3 * * * * *` is every three seconds. Unset,
every rule fails to load: that is on purpose.

| Node | Port | Login |
|---|---|---|
| KC-DC1-CRIB (tool crib, two RFID cabinets) | http://127.0.0.1:8101 | badge `nw-elena`, `nw-marco`, `nw-raj` |
| KC-DC1-DOCK (dock supplies) | http://127.0.0.1:8102 | badge `nw-dana`, `nw-owen` |
| SGF-XD2-CRIB (Springfield, one cabinet) | http://127.0.0.1:8103 | badge `nw-marco`, `nw-owen` |
| Timeclock terminal | http://127.0.0.1:8092 | |
| Controller | http://127.0.0.1:8091 | `admin@northwind.example` / `northwind-demo` |

## What you should see in the first minute

At `*/3`, each kiosk completes a transaction every three seconds.

| Where | What |
|---|---|
| `nats sub 'kiosk.*.evt.>'` | `transaction.complete` and `receipt.transaction` from all three kiosks; `item.checkout` and `item.return` at the cribs, `item.consume` at the dock; `timeclock.punch` |
| Controller, Tools Out | open checkouts growing, then levelling off at about half of each crib's pool per worker |
| Controller, timesheets | punches from all three kiosks, Marco and Owen at both sites |
| rule-router log | `409` on some punches: "already clocked in" or "not clocked in". The punch rule picks in or out at random and the kiosk refuses one that does not fit. That is the kiosk deciding, not a fault. |

A live run of this runbook produced 72 completed transactions across the three
kiosks in 75 seconds.

## The cabinet

KC-DC1-CRIB's cabinets A and B, and SGF-XD2-CRIB's cabinet A, run the real
`enclosure_diff` flow on the mock reader: each read returns the tags listed in
its file under `demo/tags/`. The badge at the cabinet door and the "door
closed" signal come from an access-control system in production; here you send
them:

```bash
# Marco badges at cabinet A
nats req kiosk.KC-DC1-CRIB.cmd.cart.start '{"user_code":"nw-marco","enclosure_id":"A"}'

# He takes the digital torque wrench
sed -i '/PT-2020-0001/d' demo/tags/kc-dc1-crib-a.txt

# The door closes: one read, and the kiosk proposes the checkout
nats req kiosk.KC-DC1-CRIB.cmd.read.trigger '{"user_code":"nw-marco","enclosure_id":"A"}'
```

The reply lists one `checkout` line for `PT-2020-0001`. Confirm it on the crib's
screen. Put the line back and read again to return it. The digital torque
wrench goes to the maintenance bench on return, by the fixture's policy.

Reset every cabinet with `git checkout demo/tags`. The mock reader needs
`rfid.allow_mock: true`, which a production config never sets.

## The standalone kiosk

One kiosk, no controller. The kiosk itself has NATS off; rule-router still
needs a NATS server for the chain between its hops.

```bash
KIOSK_CONFIG=demo/standalone.yaml ./kiosk-app demo-seed --confirm
KIOSK_CONFIG=demo/standalone.yaml ./kiosk-app serve
nats-server
DEMO_CRON="*/20 * * * * *" rule-router --config demo/rule-router.yaml --rules demo/rules/kc-dc1-crib
```

The cabinets need NATS commands, so they are off in the standalone config.

## Known noise

- **Receipts fail to send.** The controller emails a receipt per transaction
  and logs `smtp send: failed to locate a sendmail executable path` until SMTP
  is set in its PocketBase settings (`/_`). The send log records each as
  failed. Point it at a mail catcher if you want to show receipts.
- **The controller skips service discovery** until `nats.code` in
  `demo/controller.yaml` is set to its Thing's code from the platform.

## Files

| File | What it is |
|---|---|
| `kc-dc1-crib/`, `kc-dc1-dock/`, `sgf-xd2-crib/` | One folder per kiosk, so the standalone demo loads just the crib's. Each holds the three-hop cart chain and a punch rule. |
| `../rule-router.yaml` | rule-router config: scheduler, router and gateway on, publish mode core, listener on loopback. |
| `../kc-dc1-crib.yaml` and siblings | The node configs. |
| `../tags/` | The cabinets' tag files, generated from the fixture. `go test ./internal/demoseed` fails if they drift; `UPDATE_TAGS=1` rewrites them. |
