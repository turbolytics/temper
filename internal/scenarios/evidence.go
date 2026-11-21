package scenarios

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/turbolytics/temper"
)

// WriteEvidence appends one record per verdict to a new file in dir, named
// as every adapter names its file: <level>-<pid>-<8 hex>.jsonl.
func WriteEvidence(dir, level, run string, now time.Time, vs []Verdict) error {
	if len(vs) == 0 {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	suffix := make([]byte, 4)
	if _, err := rand.Read(suffix); err != nil {
		return err
	}
	name := filepath.Join(dir, fmt.Sprintf("%s-%d-%s.jsonl", level, os.Getpid(), hex.EncodeToString(suffix)))
	f, err := os.OpenFile(name, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	at := now.UTC().Format(time.RFC3339)
	for _, v := range vs {
		line, err := json.Marshal(temper.Record{
			Covers: v.Reference, Subject: v.Subject, Scenario: v.Scenario, Test: v.Test,
			Level: level, Outcome: v.Outcome, Run: run, At: at,
		})
		if err != nil {
			f.Close()
			return err
		}
		if _, err := f.Write(append(line, '\n')); err != nil {
			f.Close()
			return err
		}
	}
	return f.Close()
}
