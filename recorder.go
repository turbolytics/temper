package temper

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

// recorder collects claims per test and appends them as evidence when each
// test ends. One recorder writes one file per (evidence dir, level), named
// uniquely for the process, so parallel test processes never share a file
// and nothing is ever truncated.
type recorder struct {
	getenv func(string) string
	now    func() time.Time

	mu      sync.Mutex
	pending map[testing.TB][]claim
	files   map[string]*os.File
}

func newRecorder(getenv func(string) string, now func() time.Time) *recorder {
	return &recorder{
		getenv:  getenv,
		now:     now,
		pending: map[testing.TB][]claim{},
		files:   map[string]*os.File{},
	}
}

func (r *recorder) add(t testing.TB, exempts bool, ref string, opts []Option) {
	if r.getenv("TEMPER_LEVEL") == "" || r.getenv("TEMPER_EVIDENCE") == "" {
		return
	}
	c := claim{ref: ref, exempts: exempts}
	for _, opt := range opts {
		opt(&c)
	}

	r.mu.Lock()
	_, seen := r.pending[t]
	r.pending[t] = append(r.pending[t], c)
	r.mu.Unlock()

	if !seen {
		t.Cleanup(func() { r.finish(t) })
	}
}

// finish runs when the test ends: its outcome is known, so its claims become
// records. A panic outranks a failure, which outranks a skip; anything but a
// clean run is not a pass.
func (r *recorder) finish(t testing.TB) {
	outcome := OutcomePass
	switch {
	case panicking():
		outcome = OutcomeError
	case t.Failed():
		outcome = OutcomeFail
	case t.Skipped():
		outcome = OutcomeSkip
	}

	r.mu.Lock()
	claims := r.pending[t]
	delete(r.pending, t)
	r.mu.Unlock()

	level, dir, run := r.getenv("TEMPER_LEVEL"), r.getenv("TEMPER_EVIDENCE"), r.getenv("TEMPER_RUN")
	at := r.now().UTC().Format(time.RFC3339)
	for _, c := range claims {
		rec := Record{
			Subject:  c.subject,
			Scenario: c.scenario,
			Test:     t.Name(),
			Level:    level,
			Outcome:  outcome,
			Run:      run,
			At:       at,
		}
		if c.exempts {
			rec.Exempts = c.ref
		} else {
			rec.Covers = c.ref
		}
		if err := r.write(dir, level, rec); err != nil {
			// The test has ended, so it can no longer be failed. Say so loudly;
			// the missing record leaves its cell missing, never covered.
			fmt.Fprintf(os.Stderr, "temper: write evidence for %s: %v\n", t.Name(), err)
		}
	}
}

// panicking reports whether the caller is running while a panic unwinds.
//
// testing runs a test's cleanups from a deferred call, so when the test body
// panics they run before testing marks the test failed: t.Failed() reads
// false. The panic is still on the stack, as runtime.gopanic, and that is the
// only signal the standard library leaves. TestAdapter_PanicRecordsError
// holds this against the Go version in use.
func panicking() bool {
	pcs := make([]uintptr, 128)
	frames := runtime.CallersFrames(pcs[:runtime.Callers(2, pcs)])
	for {
		frame, more := frames.Next()
		if frame.Function == "runtime.gopanic" {
			return true
		}
		if !more {
			return false
		}
	}
}

// write appends one record as a single line in a single Write call, under the
// recorder's lock, so concurrent tests never interleave partial lines.
func (r *recorder) write(dir, level string, rec Record) error {
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	line = append(line, '\n')

	r.mu.Lock()
	defer r.mu.Unlock()
	f, err := r.file(dir, level)
	if err != nil {
		return err
	}
	_, err = f.Write(line)
	return err
}

// file returns this recorder's file for (dir, level), opening it on first use.
// Callers hold r.mu.
func (r *recorder) file(dir, level string) (*os.File, error) {
	key := dir + "\x00" + level
	if f, ok := r.files[key]; ok {
		return f, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	suffix := make([]byte, 4)
	if _, err := rand.Read(suffix); err != nil {
		return nil, err
	}
	name := filepath.Join(dir, fmt.Sprintf("%s-%d-%s.jsonl", level, os.Getpid(), hex.EncodeToString(suffix)))
	f, err := os.OpenFile(name, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	r.files[key] = f
	return f, nil
}
