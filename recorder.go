package temper

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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

	mu       sync.Mutex
	pending  map[testing.TB][]claim
	files    map[string]*os.File
	deferred bool         // set by Run: outcomes are settled after every test finishes
	finished []testing.TB // tests that ended while deferred, awaiting settle
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

	if seen {
		return
	}
	r.mu.Lock()
	deferred := r.deferred
	r.mu.Unlock()
	if !deferred && hasCleanups(t) {
		// Cleanups run last in, first out, so one registered before this claim
		// runs after the adapter has read the outcome. A failure in it would be
		// recorded as a pass. Refuse rather than guess.
		t.Errorf("temper: %s calls Covers after registering a cleanup (directly, or through a mock, t.TempDir or t.Setenv); "+
			"a failure in that cleanup would be missed. Call temper.Covers first in the test, or run the package's tests "+
			"with temper.Main from TestMain", t.Name())
	}
	t.Cleanup(func() { r.finish(t) })
}

// finish runs when the test ends: its outcome is known, so its claims become
// records. A panic outranks a failure, which outranks a skip; anything but a
// clean run is not a pass.
func (r *recorder) finish(t testing.TB) {
	if panicking() {
		// The panic ends the process before Run could settle, so write now.
		r.write(t, OutcomeError)
		return
	}
	r.mu.Lock()
	if r.deferred {
		r.finished = append(r.finished, t)
		r.mu.Unlock()
		return
	}
	r.mu.Unlock()
	r.write(t, outcome(t))
}

// outcome reads how a test ended. A failure outranks a skip.
func outcome(t testing.TB) string {
	switch {
	case t.Failed():
		return OutcomeFail
	case t.Skipped():
		return OutcomeSkip
	}
	return OutcomePass
}

// settle writes the claims of every test that ended while deferred. Each
// test has fully finished, cleanups included, so its outcome is final.
func (r *recorder) settle() {
	r.mu.Lock()
	done := r.finished
	r.finished = nil
	r.deferred = false
	r.mu.Unlock()
	for _, t := range done {
		r.write(t, outcome(t))
	}
}

// write turns a finished test's claims into records.
func (r *recorder) write(t testing.TB, outcome string) {
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
		if err := r.append(dir, level, rec); err != nil {
			// The test has ended, so it can no longer be failed. Say so loudly;
			// the missing record leaves its cell missing, never covered.
			fmt.Fprintf(os.Stderr, "temper: write evidence for %s: %v\n", t.Name(), err)
		}
	}
}

// hasCleanups reports whether a real *testing.T or *testing.B already has
// cleanups registered. testing offers no API for this, so it reads the
// unexported field; a fake TB, or a Go release that renames the field, reads
// as none. TestAdapter_CleanupBeforeCoversNeverPasses holds this against the
// Go version in use.
func hasCleanups(t testing.TB) bool {
	v := reflect.ValueOf(t)
	if v.Kind() != reflect.Pointer || v.IsNil() || v.Elem().Kind() != reflect.Struct {
		return false
	}
	common := v.Elem().FieldByName("common")
	if !common.IsValid() || common.Kind() != reflect.Struct {
		return false
	}
	cleanups := common.FieldByName("cleanups")
	return cleanups.IsValid() && cleanups.Kind() == reflect.Slice && cleanups.Len() > 0
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

// append writes one record as a single line in a single Write call, under the
// recorder's lock, so concurrent tests never interleave partial lines.
func (r *recorder) append(dir, level string, rec Record) error {
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
