package demoseed_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stone-age-io/kiosk/internal/demoseed"
)

// tagDir holds one tag file per demo cabinet, read by the mock RFID reader
// (rfid.readers.<id>.tag_file). They are committed rather than written by
// demo-seed because a managed kiosk never runs demo-seed: the controller
// installs its units over the bus. The EPCs are derived from the unit codes,
// so the files are a pure function of the fixture.
const tagDir = "../../demo/tags"

// tagFileName is where a node's cabinet lists its tags.
func tagFileName(node, enclosure string) string {
	return strings.ToLower(node) + "-" + strings.ToLower(enclosure) + ".txt"
}

func renderTagFile(n demoseed.Node, enclosure string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s cabinet %s: the tags a mock reader reports present.\n", n.Code, enclosure)
	b.WriteString("# Delete a line to take that unit out, then trigger a read.\n")
	b.WriteString("# Reset with: git checkout demo/tags\n")
	for _, u := range n.Units {
		if u.EnclosureID != enclosure {
			continue
		}
		name := u.ItemCode
		if it, ok := demoseed.ItemByCode(u.ItemCode); ok {
			name = it.Name
		}
		fmt.Fprintf(&b, "%s  # %s %s\n", u.EPC(), u.Code, name)
	}
	return b.String()
}

// The committed tag files must list exactly the fixture's units, cabinet by
// cabinet; otherwise a freshly seeded cabinet would read as already emptied.
// Run with UPDATE_TAGS=1 to rewrite them after changing the fixture.
func TestDemoTagFilesMatchTheFixture(t *testing.T) {
	update := os.Getenv("UPDATE_TAGS") == "1"
	want := map[string]bool{}
	for _, n := range demoseed.Nodes {
		for _, enc := range n.EnclosureIDs() {
			name := tagFileName(n.Code, enc)
			want[name] = true
			path := filepath.Join(tagDir, name)
			body := renderTagFile(n, enc)
			if update {
				if err := os.MkdirAll(tagDir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
				continue
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Errorf("%s: %v (run with UPDATE_TAGS=1 to write it)", path, err)
				continue
			}
			if strings.ReplaceAll(string(got), "\r\n", "\n") != body {
				t.Errorf("%s does not match the fixture. If you emptied a cabinet during a demo, `git checkout demo/tags`; if the fixture changed, run with UPDATE_TAGS=1", path)
			}
		}
	}
	entries, _ := os.ReadDir(tagDir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".txt") && !want[e.Name()] {
			t.Errorf("%s is not a cabinet in the fixture", filepath.Join(tagDir, e.Name()))
		}
	}
}
