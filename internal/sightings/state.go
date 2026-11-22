package sightings

import "time"

// The controller broadcasts each serialized unit's latest sighting into the
// seen family of kvx.StateBucket, keyed "<kiosk_code>.<instance_id>". Written
// by the controller's SightingIngest; watched OWN-SLICE-ONLY by managed nodes
// via MirrorWatcher (Watch("<code>.>"), the items pattern — NOT the punch
// WatchAll pattern), so a node sees sightings of its own units made by other
// nodes' gateways without ever receiving another site's keys. Advisory,
// last-write-wins.

// LastObservedState is the KV value shape for one unit's latest sighting.
type LastObservedState struct {
	Zone       string    `json:"zone,omitempty"`
	Gateway    string    `json:"gateway,omitempty"`
	Lat        float64   `json:"lat,omitempty"`
	Lon        float64   `json:"lon,omitempty"`
	ObservedAt time.Time `json:"observed_at"`
}

// LastObservedStateKey builds the bucket key for one unit. The instance id (the
// owning node's item_instances.id) is the suffix so the node mirror can stamp
// by id directly, no code→id lookup.
func LastObservedStateKey(kioskCode, instanceID string) string {
	return kioskCode + "." + instanceID
}
