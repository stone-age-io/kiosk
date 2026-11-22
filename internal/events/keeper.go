package events

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stone-age-io/kit/credskeeper"
	"github.com/stone-age-io/kit/health"

	"github.com/stone-age-io/kiosk/internal/config"
)

// firstCredsWait bounds how long a first start waits for the platform to
// write the creds file. Past it the connect that follows has no file: a node or
// a timeclock serves without NATS until a restart, and the controller, which
// needs NATS, exits. A restart that finds the file goes on at once,
// even while the platform is down.
const firstCredsWait = 30 * time.Second

// StartKeeper starts the kit's credskeeper for cfg.Platform (ADR 0009) when it
// is set and serving is true, and returns nil otherwise, so `migrate` or
// `demo-seed` never waits on the platform. It keeps cfg.NATS.CredentialsFile
// current until ctx ends, and waits up to firstCredsWait for the first file,
// since nats.go reads the file before it dials. Call it before Connect.
//
// dataDir is PocketBase's data directory: the creds file and the session are
// refused inside it, because PocketBase backups archive it whole. Their own
// directory is made here, 0700, since the keeper writes only into one that
// exists.
//
// The kiosk binaries serve no Prometheus endpoint, so the keeper's metrics go
// to a registry nobody scrapes; the credentials and platform readiness checks
// (Checks) are where its state shows.
func StartKeeper(ctx context.Context, cfg *config.Config, dataDir string, serving bool) (*credskeeper.Keeper, error) {
	if !cfg.Platform.Enabled() || !serving {
		return nil, nil
	}
	if err := cfg.CredsOutside(dataDir); err != nil {
		return nil, err
	}
	for _, f := range []string{cfg.NATS.CredentialsFile, cfg.Platform.SessionFile} {
		if f == "" {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
			return nil, fmt.Errorf("platform: %w", err)
		}
	}
	k, err := credskeeper.New(credskeeper.Config{
		URL:           cfg.Platform.URL,
		Identity:      cfg.Platform.Identity,
		Password:      cfg.Platform.Password,
		CredsFile:     cfg.NATS.CredentialsFile,
		SessionFile:   cfg.Platform.SessionFile,
		SyncInterval:  cfg.Platform.SyncInterval.AsDuration(),
		AllowInsecure: cfg.Platform.AllowInsecure,
	}, slog.Default().With("component", "credskeeper"), credskeeper.NewMetrics(prometheus.NewRegistry()))
	if err != nil {
		return nil, fmt.Errorf("platform: %w", err)
	}
	go k.Run(ctx)

	waitCtx, cancel := context.WithTimeout(ctx, firstCredsWait)
	defer cancel()
	if err := k.WaitForCreds(waitCtx); err != nil {
		slog.Warn("platform.no_creds_yet: no creds file from the platform, so NATS cannot connect; the keeper keeps trying (see the credentials and platform checks, and the logged sync error)", "error", err)
	}
	return k, nil
}

// Checks returns the keeper's readiness registrations for readiness.Serve,
// none when k is nil.
func Checks(k *credskeeper.Keeper) []func(*health.Registry) {
	if k == nil {
		return nil
	}
	return []func(*health.Registry){k.RegisterChecks}
}
