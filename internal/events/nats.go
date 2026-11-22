package events

import (
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	kitnatsx "github.com/stone-age-io/kit/natsx"

	"github.com/stone-age-io/kiosk/internal/config"
)

// Publisher is the interface SetPublisher accepts. The production
// implementation wraps a nats.Conn; tests inject their own to capture
// calls. Kept tiny on purpose — we only publish, we never consume.
//
// PublishBytes takes the already-marshaled JSON so events.Publish can
// marshal once and reuse the bytes for both the slog line and the wire
// publish. Callers shouldn't need to know what the bytes mean — the
// publisher just forwards them to NATS.
type Publisher interface {
	PublishBytes(subject string, data []byte) error
	Close()
}

// natsPublisher is the production implementation. PublishBytes is a thin
// wrapper around nats.Conn.Publish, which buffers internally. Errors are
// returned to the caller, which logs them; we don't retry — the NATS
// client's own reconnect logic handles transient disconnects.
type natsPublisher struct {
	nc *nats.Conn
}

func (p *natsPublisher) PublishBytes(subject string, data []byte) error {
	return p.nc.Publish(subject, data)
}

// PublishEvent publishes an event with its Nats-Msg-Id header, so the stream
// stores it once within its duplicate window (ADR 0008 rule 3). Still a core
// publish: the client buffers it while disconnected, as before.
func (p *natsPublisher) PublishEvent(subject, msgID string, data []byte) error {
	msg := &nats.Msg{Subject: subject, Data: data, Header: nats.Header{}}
	msg.Header.Set(jetstream.MsgIDHeader, msgID)
	return p.nc.PublishMsg(msg)
}

func (p *natsPublisher) Close() {
	if p == nil || p.nc == nil {
		return
	}
	// Drain only works on a connected conn; if we're still mid-reconnect
	// (e.g., never reached the server since startup), Drain errors. In that
	// case just close — there's nothing usefully drainable.
	if p.nc.IsConnected() {
		if err := p.nc.Drain(); err != nil {
			slog.Warn("kiosk.nats.drain_failed", "error", err)
		}
		return
	}
	p.nc.Close()
}

// Conn exposes the underlying *nats.Conn for callers that need JetStream,
// KV, or a consumer (e.g., the kiosk's catalog watcher and the controller).
// Returning the live conn is safe because both Conn() and PublishJSON share
// it — there's no parallel state to coordinate.
func (p *natsPublisher) Conn() *nats.Conn { return p.nc }

// JetStream returns a jetstream.JetStream context from the active publisher.
// Errors when the publisher is nil/disabled (NATS not enabled) or when it's
// a non-NATS test fake. Callers that need JS during startup should treat a
// nil err + nil JS as "JS not available, run in degraded mode."
func JetStream(p Publisher) (jetstream.JetStream, error) {
	nc, err := Conn(p)
	if err != nil {
		return nil, err
	}
	return jetstream.New(nc)
}

// Conn exposes the underlying *nats.Conn from the active publisher. Callers
// that need a subscription (command bus, heartbeat sub) use this rather than
// the JetStream context. Errors when the publisher is nil/disabled or is a
// non-NATS test fake. Mirrors JetStream's shape so wiring code reads the same.
func Conn(p Publisher) (*nats.Conn, error) {
	if p == nil {
		return nil, errors.New("nats publisher is nil — events.enabled likely false")
	}
	type connHolder interface{ Conn() *nats.Conn }
	holder, ok := p.(connHolder)
	if !ok {
		return nil, errors.New("publisher does not expose a NATS connection")
	}
	nc := holder.Conn()
	if nc == nil {
		return nil, errors.New("publisher has no NATS connection")
	}
	return nc, nil
}

// Connect dials NATS using the auth knobs the operator filled in. Returns
// (nil, nil) when cfg.Enabled is false — main.go treats nil as a no-op
// publisher.
//
// name is the client identifier reported to nats-server (visible in its
// connection logs and `nats server connections` output). Pass
// "kiosk-<kiosk_code>" from the kiosk binary and "kiosk-controller" from
// the controller binary so a shared NATS server can tell them apart.
// Empty string falls back to "kiosk".
//
// Network unreachability is NOT an error: with RetryOnFailedConnect, the
// returned *nats.Conn enters a connecting/buffering state and dials in
// the background. The kiosk's primary job is local checkout against the
// ledger; NATS is a best-effort feed to the central reporting consumer
// and should never block the kiosk from starting. An (nil, err) return
// here means a structural config problem (empty URL, malformed URL,
// unloadable creds) that won't fix itself by waiting.
func Connect(cfg config.NATSConfig, name string) (Publisher, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if cfg.URL == "" {
		return nil, errors.New("nats.enabled=true but nats.url is empty")
	}

	if name == "" {
		name = "kiosk"
	}
	extra, err := authOptions(cfg)
	if err != nil {
		return nil, err
	}
	// The kit's Connect reconnects forever, keeps trying through auth errors
	// (so a creds file creds-keeper rewrites after a Revoke is picked up), and
	// logs every connection event. RetryOnFailedConnect makes the *initial*
	// dial behave the same way: if the server isn't reachable at startup, it
	// still returns a connection that buffers publishes and dials in the
	// background. That is what lets the kiosk boot even when the central NATS
	// endpoint is down — the local ledger is authoritative regardless.
	nc, err := kitnatsx.Connect(kitnatsx.Options{
		URLs:                 []string{cfg.URL},
		Creds:                cfg.CredentialsFile,
		Name:                 name,
		Log:                  slog.Default(),
		RetryOnFailedConnect: true,
		Extra:                append(extra, nats.ReconnectWait(2*time.Second)),
	})
	if err != nil {
		return nil, err
	}
	return &natsPublisher{nc: nc}, nil
}

// authOptions translates the config's auth and TLS knobs into nats.Options.
// They compose so an operator can mix (e.g., TLS + nkey). A creds file is not
// here: the kit's Connect takes it directly. Returns an error only for a
// structural problem an operator must fix (e.g., an unloadable nkey seed).
func authOptions(cfg config.NATSConfig) ([]nats.Option, error) {
	var opts []nats.Option

	// Auth modes — apply each that's configured. nats.go is happy with
	// multiple options; the server enforces what it actually requires.
	if cfg.Token != "" {
		opts = append(opts, nats.Token(cfg.Token))
	}
	if cfg.Username != "" || cfg.Password != "" {
		opts = append(opts, nats.UserInfo(cfg.Username, cfg.Password))
	}
	if cfg.NKeySeedFile != "" {
		opt, err := nats.NkeyOptionFromSeed(cfg.NKeySeedFile)
		if err != nil {
			// The operator explicitly asked for nkey auth. Silently
			// continuing without it would connect unauthenticated (if the
			// server permits) or fail the handshake later with a confusing
			// error far from the real cause — a bad seed path/contents. Fail
			// loud, the same way an unloadable creds file does via nats.Connect.
			return nil, fmt.Errorf("load nats nkey seed %q: %w", cfg.NKeySeedFile, err)
		}
		opts = append(opts, opt)
	}

	// TLS — explicit knobs override the URL scheme's default.
	if cfg.TLSCAFile != "" {
		opts = append(opts, nats.RootCAs(cfg.TLSCAFile))
	}
	if cfg.TLSCertFile != "" && cfg.TLSKeyFile != "" {
		opts = append(opts, nats.ClientCert(cfg.TLSCertFile, cfg.TLSKeyFile))
	}
	if cfg.TLSInsecure {
		opts = append(opts, nats.Secure(&tls.Config{InsecureSkipVerify: true})) //nolint:gosec
	}

	return opts, nil
}
