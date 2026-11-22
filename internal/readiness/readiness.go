// Package readiness wires GET /api/ready, the platform's readiness contract,
// into each kiosk binary. The engine, the wire format and the database checks
// are stone-age-io/kit's; each binary passes its own collections and NATS
// check.
package readiness

import (
	"context"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"github.com/stone-age-io/kit/health"
	"github.com/stone-age-io/kit/pbx"
	"github.com/stone-age-io/kit/version"
)

// The collections each binary cannot serve without. When one is missing, the
// binary's migrations never registered: PocketBase's own collections exist
// and the console loads, but nothing of ours is there.
var (
	NodeCollections       = []string{"items", "transactions", "open_checkouts", "admins"}
	TimeclockCollections  = []string{"time_punches", "admins"}
	ControllerCollections = []string{"kiosks", "kiosk_items", "admins"}
)

// Register adds the checks for one binary to reg: the database answers, its
// collections exist, the database is not ahead of the binary, and its NATS
// check.
func Register(reg *health.Registry, app core.App, natsCheck health.Func, collections []string) {
	reg.Register("database", pbx.Database(app))
	reg.Register("schema", pbx.Schema(app, collections...))
	reg.Register("schema_version", pbx.SchemaVersion(app))
	reg.Register("nats", natsCheck)
}

// Serve registers the checks, routes GET /api/ready to a cached prober, and
// starts it until ctx is done. Call it from OnServe, after NATS is connected,
// so the first probe sees the connection. extra adds further checks, such as
// the platform credential keeper's (events.Checks).
func Serve(ctx context.Context, se *core.ServeEvent, natsCheck health.Func, collections []string, extra ...func(*health.Registry)) {
	var reg health.Registry
	Register(&reg, se.App, natsCheck, collections)
	for _, add := range extra {
		add(&reg)
	}
	p := health.NewProber(&reg, version.Build(), 0, 0)
	pbx.ServeReady(se, p)
	p.Start(ctx)
}
