// Package evidence reads the records tests wrote, and holds each one to the
// declarations before it may credit a cell.
package evidence

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/turbolytics/temper"
	"github.com/turbolytics/temper/internal/diag"
	"github.com/turbolytics/temper/internal/model"
)

// Located is an evidence record and where it was read from.
type Located struct {
	temper.Record
	File string // relative to the project root
	Line int
}

// Where is the record's position, as `file:line`.
func (l Located) Where() string { return fmt.Sprintf("%s:%d", l.File, l.Line) }

// Read returns every record in the evidence directory's *.jsonl files, in
// file-name then line order. A missing directory holds no evidence. A line
// that is not a record is an error; the rest of the file is still read.
func Read(p *model.Project) ([]Located, diag.List) {
	dir := p.EvidenceDir()
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	rel := func(name string) string {
		r, rerr := filepath.Rel(p.Root, filepath.Join(dir, name))
		if rerr != nil {
			return filepath.ToSlash(filepath.Join(dir, name))
		}
		return filepath.ToSlash(r)
	}
	if err != nil {
		return nil, diag.List{{File: rel(""), Message: err.Error(), Rule: "R7"}}
	}

	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	var recs []Located
	var errs diag.List
	for _, name := range names {
		file := rel(name)
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			errs = append(errs, diag.Error{File: file, Message: err.Error(), Rule: "R7"})
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64*1024), 1<<20)
		for n := 1; sc.Scan(); n++ {
			line := bytes.TrimRight(sc.Bytes(), "\r")
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			r, err := parseRecord(line)
			if err != nil {
				errs = append(errs, diag.Error{File: fmt.Sprintf("%s:%d", file, n), Message: "is not an evidence record: " + err.Error(), Rule: "R7"})
				continue
			}
			recs = append(recs, Located{Record: r, File: file, Line: n})
		}
		if err := sc.Err(); err != nil {
			errs = append(errs, diag.Error{File: file, Message: err.Error(), Rule: "R7"})
		}
		f.Close()
	}
	return recs, errs
}

// recordKeys are the keys a record may carry, spelled exactly.
var recordKeys = map[string]bool{
	"covers": true, "exempts": true, "subject": true, "scenario": true,
	"test": true, "level": true, "outcome": true, "run": true, "at": true,
}

// parseRecord reads a line holding exactly one JSON object whose keys are
// record keys, each spelled exactly and given once. encoding/json alone
// would accept a second object or trailing text, match keys in any case,
// and let a repeated key win silently.
func parseRecord(line []byte) (temper.Record, error) {
	var r temper.Record
	dec := json.NewDecoder(bytes.NewReader(line))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return r, errors.New("not a JSON object")
	}
	seen := map[string]bool{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return r, err
		}
		key, _ := tok.(string)
		switch {
		case !recordKeys[key]:
			return r, fmt.Errorf("unknown key %q", key)
		case seen[key]:
			return r, fmt.Errorf("key %q given twice", key)
		}
		seen[key] = true
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return r, err
		}
	}
	if _, err := dec.Token(); err != nil { // the closing brace
		return r, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return r, errors.New("more than one JSON value on the line")
	}
	if err := json.Unmarshal(line, &r); err != nil {
		return r, err
	}
	return r, nil
}

// Clean removes every *.jsonl file in the evidence directory and returns how
// many it removed. Other files, and the directory, stay.
func Clean(p *model.Project) (int, error) {
	names, err := filepath.Glob(filepath.Join(p.EvidenceDir(), "*.jsonl"))
	if err != nil {
		return 0, err
	}
	for i, name := range names {
		if err := os.Remove(name); err != nil {
			return i, err
		}
	}
	return len(names), nil
}
