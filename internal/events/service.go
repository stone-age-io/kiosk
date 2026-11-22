package events

import (
	"context"
	"log/slog"
	"os"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"
	"github.com/stone-age-io/kit/health"
	kitnatsx "github.com/stone-age-io/kit/natsx"
)

// The discovery name of each binary. Every copy of a binary shares its name,
// which is what lets one $SRV.PING.<name> find all of them.
const (
	ServiceKiosk      = "kiosk"
	ServiceTimeclock  = "kiosk-timeclock"
	ServiceController = "kiosk-controller"
)

// RegisterService announces this process on NATS service discovery through
// the kit, so `nats micro ls` shows what runs, at which version, and on which
// Thing: code is the process's Thing code, carried as `code` metadata (ADR
// 0008 rule 4). It is identity only, with no endpoints. Never fatal: with NATS
// disabled, or no code, or any failure, it logs and returns nil. Stop the
// returned service before closing the connection.
func RegisterService(p Publisher, name, description, code string) micro.Service {
	if p == nil {
		return nil
	}
	nc, err := Conn(p)
	if err != nil {
		return nil
	}
	meta := map[string]string{}
	if host, err := os.Hostname(); err == nil {
		meta["host"] = host
	}
	svc, err := kitnatsx.RegisterService(nc, kitnatsx.Service{
		Name:        name,
		Description: description,
		Code:        code,
		Metadata:    meta,
	})
	if err != nil {
		slog.Warn("kiosk.nats.discovery_skipped", "service", name, "error", err)
		return nil
	}
	slog.Info("kiosk.nats.discovery_registered", "service", name, "code", code)
	return svc
}

// NATSReady is the readiness check for a kiosk node's NATS connection, which
// is best-effort: a node keeps taking checkouts against its local ledger
// without NATS, so a lost connection warns and never fails (failing would take
// a working kiosk out of rotation). Disabled NATS is skipped.
func NATSReady(p Publisher) health.Func {
	return func(context.Context) health.Result {
		if p == nil {
			return health.Skip("nats.enabled is false")
		}
		nc, err := Conn(p)
		if err != nil {
			return health.Skip(err.Error())
		}
		if nc.IsConnected() {
			return health.OK("connected to " + nc.ConnectedUrlRedacted())
		}
		return health.Warn("not connected ("+nc.Status().String()+"): events are buffering until it returns",
			"Check nats.url and the credentials, and that the server is reachable.")
	}
}

// ControllerNATSReady is the controller's NATS check, which fails: the
// controller does nothing without the bus.
func ControllerNATSReady(p Publisher) health.Func {
	return kitnatsx.Ready(func() *nats.Conn {
		nc, _ := Conn(p)
		return nc
	})
}
