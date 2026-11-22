package readiness

import (
	"context"
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tests"
	"github.com/stone-age-io/kit/health"
	"github.com/stone-age-io/kit/health/healthtest"

	_ "github.com/stone-age-io/kiosk/migrations"
)

// A node's readiness on a migrated database with NATS disabled: the database
// checks pass, NATS is skipped, the node is ready, and the answer follows the
// shared wire format before and after the first probe.
func TestNodeReadinessConforms(t *testing.T) {
	// A fresh data directory, not PocketBase's bundled test data, whose
	// migrations no binary registers.
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)

	natsOff := func(context.Context) health.Result { return health.Skip("nats.enabled is false") }
	var reg health.Registry
	Register(&reg, app, natsOff, NodeCollections)
	p := health.NewProber(&reg, "v0.0.0-test", time.Hour, 5*time.Second)
	healthtest.Conform(t, p.Handler())
	p.Start(t.Context())
	healthtest.Conform(t, p.Handler())

	rep := p.Snapshot()
	if !rep.Ready {
		t.Errorf("a migrated node with NATS off should be ready: %+v", rep.Checks)
	}
	for _, c := range rep.Checks {
		want := health.StateOK
		if c.Name == "nats" {
			want = health.StateSkipped
		}
		if c.State != want {
			t.Errorf("%s = %s (%s), want %s", c.Name, c.State, c.Detail, want)
		}
	}
}
