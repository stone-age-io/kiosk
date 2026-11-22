package rfid

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/stone-age-io/kiosk/internal/config"
)

// The mock reader reports the file's EPCs: comments, blank lines and repeats
// are dropped, and deleting a line is a tag leaving the cabinet.
func TestFileReaderReadsTheTagFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cabinet-a.txt")
	write := func(s string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("# cabinet A\ne2801160aaaa\n\n  e2801160bbbb  # torque wrench\ne2801160aaaa\n")

	r, err := New(config.RFIDReaderConfig{Mode: config.RFIDModeEnclosureDiff, EnclosureID: "A", TagFile: path})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Connect(); err != nil {
		t.Fatal(err)
	}
	if !r.Connected() {
		t.Fatal("Connected() = false with the file present")
	}
	got, err := r.ReadFor(context.Background(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if want := []EPC{"e2801160aaaa", "e2801160bbbb"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ReadFor = %v, want %v", got, want)
	}

	write("e2801160aaaa\n") // the wrench leaves
	got, _ = r.ReadFor(context.Background(), time.Second)
	if want := []EPC{"e2801160aaaa"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after deleting a line ReadFor = %v, want %v", got, want)
	}

	write("") // an empty cabinet is an empty read, not an error
	if got, err = r.ReadFor(context.Background(), time.Second); err != nil || len(got) != 0 {
		t.Fatalf("empty file: ReadFor = %v, %v", got, err)
	}
}

// A missing file is a reader that is not there: an error from ReadFor and
// Connected() false, never an empty read that would check every tag out.
func TestFileReaderMissingFileIsAnError(t *testing.T) {
	r := newFileReader(filepath.Join(t.TempDir(), "nope.txt"))
	if r.Connected() {
		t.Fatal("Connected() = true with no file")
	}
	if _, err := r.ReadFor(context.Background(), time.Second); err == nil {
		t.Fatal("ReadFor on a missing file returned no error")
	}
}
