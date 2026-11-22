package controller

// Side-effect imports that register both kiosk and controller-only
// migrations with the global PB AppMigrations list. setupApp in
// consumer_test.go runs the resulting set against a fresh per-test DB.
import (
	_ "github.com/stone-age-io/kiosk/migrations"
	_ "github.com/stone-age-io/kiosk/migrations/controller"
)
