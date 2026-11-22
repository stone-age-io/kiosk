package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPlatformFromYAMLAndEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "kiosk.yaml")
	yaml := `kiosk: {code: K1, location_code: L1}
nats: {enabled: true, url: "nats://hub:4222", credentials_file: pb_creds/kiosk.creds}
platform:
  url: https://platform.example.com
  identity: k1@ops.thing.local
  sync_interval: 2m
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KIOSK_PLATFORM_PASSWORD", "first-start-only")
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	p := c.Platform
	if !p.Enabled() || p.Identity != "k1@ops.thing.local" || p.Password != "first-start-only" || p.SyncInterval.AsDuration() != 2*time.Minute {
		t.Fatalf("platform = %+v", p)
	}
}

func TestPlatformRefusals(t *testing.T) {
	for name, tc := range map[string]struct {
		set  func(c *Config)
		want string
	}{
		"nats off":      {func(c *Config) { c.NATS.Enabled = false }, "nats.enabled"},
		"no creds file": {func(c *Config) { c.NATS.CredentialsFile = "" }, "credentials_file"},
		"second auth":   {func(c *Config) { c.NATS.Token = "t" }, "another NATS auth"},
		"negative sync": {func(c *Config) { c.Platform.SyncInterval = Duration(-time.Second) }, "sync_interval"},
	} {
		t.Run(name, func(t *testing.T) {
			c := baseKiosk()
			c.NATS.Enabled = true
			c.NATS.CredentialsFile = "pb_creds/kiosk.creds"
			c.Platform.URL = "https://platform.example.com"
			tc.set(c)
			err := validate(c)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want one naming %q", err, tc.want)
			}
		})
	}
}

func TestCredsOutside(t *testing.T) {
	c := baseKiosk()
	c.NATS.CredentialsFile = "pb_creds/kiosk.creds"
	if err := c.CredsOutside("pb_data"); err != nil {
		t.Fatalf("pb_creds refused: %v", err)
	}
	c.NATS.CredentialsFile = "pb_data_timeclock/kiosk.creds"
	if err := c.CredsOutside("pb_data"); err != nil {
		t.Fatalf("a sibling with a shared prefix refused: %v", err)
	}
	c.NATS.CredentialsFile = "pb_data/kiosk.creds"
	if err := c.CredsOutside("pb_data"); err == nil {
		t.Fatal("a creds file in pb_data accepted")
	}
	c.NATS.CredentialsFile = "pb_creds/kiosk.creds"
	c.Platform.SessionFile = "pb_data/session.json"
	if err := c.CredsOutside("pb_data"); err == nil {
		t.Fatal("a session file in pb_data accepted")
	}
}

func TestIsServe(t *testing.T) {
	for args, want := range map[string]bool{
		"kiosk":                     true,
		"kiosk --dir=x":             true,
		"kiosk serve":               true,
		"kiosk migrate up":          false,
		"kiosk demo-seed --confirm": false,
	} {
		if got := IsServe(strings.Fields(args)); got != want {
			t.Errorf("IsServe(%q) = %v, want %v", args, got, want)
		}
	}
}
