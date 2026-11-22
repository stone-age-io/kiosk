package rfid

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

// fileReader is the demo's mock reader: ReadFor returns the EPCs listed in a
// text file, one per line, with blank lines and `#` comments ignored. Taking a
// tag out of a cabinet is deleting its line; the next read reports it gone and
// the kiosk's own diff (diff.go) does the rest, exactly as for an LLRP reader.
//
// It has no population model, no randomness and no clock, on purpose: the demo
// plan's rule is "don't simulate the operator". A presenter deleting a line is
// the stimulus, and everything downstream is real.
//
// config.validateRFID refuses a tag_file unless rfid.allow_mock is true, so a
// production config cannot acquire one by a typo.
type fileReader struct {
	path string
}

func newFileReader(path string) *fileReader { return &fileReader{path: path} }

// Connect has nothing to dial. The file is read on every ReadFor, so it may be
// created or replaced at any time.
func (f *fileReader) Connect() error { return nil }

// ReadFor returns the file's EPCs. It ignores the read window: an inventory
// cycle against a file is instant, and waiting out the window would only slow
// the command reply it runs inside.
func (f *fileReader) ReadFor(ctx context.Context, _ time.Duration) ([]EPC, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fh, err := os.Open(f.path)
	if err != nil {
		return nil, fmt.Errorf("rfid: mock reader: %w", err)
	}
	defer fh.Close()

	var out []EPC
	seen := map[EPC]bool{}
	sc := bufio.NewScanner(fh)
	for sc.Scan() {
		line := sc.Text()
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		e := EPC(line)
		if !seen[e] { // a real reader reports a tag once per cycle
			seen[e] = true
			out = append(out, e)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("rfid: mock reader: %w", err)
	}
	return out, nil
}

func (f *fileReader) Close() error { return nil }

// Connected reports whether the file can be read, which is the mock's whole
// notion of a session.
func (f *fileReader) Connected() bool {
	_, err := os.Stat(f.path)
	return err == nil
}
