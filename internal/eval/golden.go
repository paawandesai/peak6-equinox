// Package eval measures the matcher against the golden set (FR-18, NFR-06,
// D-023): the human-confirmed labels in testdata/golden/labels.yaml. It also
// drafts the candidate pairs the human labels (draft.go) and computes
// precision and recall per relationship type (metrics.go).
//
// Labels are files in the repo, never DB rows, so they are reviewable
// (architecture.md). Two files share one layout rule, strict YAML with no
// unknown keys:
//
//	candidates.yaml  drafted by `equinox golden draft`; machine output, the
//	                 rules-suggested type is a hint, never a label
//	labels.yaml      written by the human only (Tier 3); every entry names who
//	                 confirmed it and on what date
//
// The settlement back-test lives in backtest*.go (M2-06).
package eval

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// File layout constants.
const (
	// Version is the only labels/candidates schema version this build reads.
	Version = 1
	// MaxFileBytes caps a golden file; forty pairs are a few kilobytes.
	MaxFileBytes = 4 << 20
	// LabelsFileName is the basename the draft command must never write.
	LabelsFileName = "labels.yaml"
	// DateLayout is the only accepted confirmed_on form: a calendar date.
	DateLayout = "2006-01-02"
	// MaxNoteLen caps free text per entry.
	MaxNoteLen = 2000
)

// ErrLabelsPath is returned when a write would target labels.yaml.
var ErrLabelsPath = errors.New("refusing to write " + LabelsFileName + ": labels are written by the human only (Tier 3)")

// Candidate is one drafted pair for the human to label. Suggested is what the
// rules said; it is a hint for the reviewer, not a label.
type Candidate struct {
	AID        string                     `yaml:"a_id"`
	ATitle     string                     `yaml:"a_title"`
	BID        string                     `yaml:"b_id"`
	BTitle     string                     `yaml:"b_title"`
	Category   canonical.Category         `yaml:"category"`
	Suggested  canonical.RelationshipType `yaml:"suggested"`
	Confidence string                     `yaml:"confidence"`
	// Why says which selection rule picked the pair (draft.go).
	Why string `yaml:"why"`
	// Vetoes summarises each hard veto as "code field: detail".
	Vetoes []string `yaml:"vetoes"`
	// Facts lists the ids of the facts the rules consulted.
	Facts       []string `yaml:"facts,omitempty"`
	Explanation string   `yaml:"explanation"`
}

// Candidates is the drafted file.
type Candidates struct {
	Version    int              `yaml:"version"`
	RoundID    int64            `yaml:"round_id"`
	DraftedAt  time.Time        `yaml:"drafted_at"`
	Method     canonical.Method `yaml:"method"`
	Candidates []Candidate      `yaml:"candidates"`
}

// Label is one human-confirmed pair.
type Label struct {
	AID string `yaml:"a_id"`
	BID string `yaml:"b_id"`
	// Label is the confirmed relationship type.
	Label canonical.RelationshipType `yaml:"label"`
	// ConfirmedBy names the human who confirmed the label. Required.
	ConfirmedBy string `yaml:"confirmed_by"`
	// ConfirmedOn is the calendar date of the confirmation. Required.
	ConfirmedOn string `yaml:"confirmed_on"`
	Note        string `yaml:"note,omitempty"`
}

// Labels is the human-owned file.
type Labels struct {
	Version int `yaml:"version"`
	// RoundID is the round the labelled pairs were drafted from (provenance).
	RoundID int64   `yaml:"round_id"`
	Labels  []Label `yaml:"labels"`
}

// PairKey identifies an unordered pair, so a label written in either venue
// order matches the matcher's (Kalshi, Polymarket) order.
func PairKey(aID, bID string) string {
	if bID < aID {
		aID, bID = bID, aID
	}
	return aID + "|" + bID
}

// Validate checks the schema version and every entry; it never reads files.
func (l Labels) Validate() error {
	var errs []error
	if l.Version != Version {
		errs = append(errs, fmt.Errorf("labels version %d is not supported (want %d)", l.Version, Version))
	}
	seen := map[string]int{}
	for i, e := range l.Labels {
		if err := e.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("labels[%d]: %w", i, err))
			continue
		}
		k := PairKey(e.AID, e.BID)
		if j, dup := seen[k]; dup {
			errs = append(errs, fmt.Errorf("labels[%d]: duplicate of labels[%d] (%s, %s)", i, j, e.AID, e.BID))
		}
		seen[k] = i
	}
	return errors.Join(errs...)
}

// Validate checks one label: valid distinct instrument ids, a valid type,
// a confirmer and a calendar date.
func (e Label) Validate() error {
	var errs []error
	for _, id := range []string{e.AID, e.BID} {
		if err := canonical.ValidateInstrumentID(id); err != nil {
			errs = append(errs, err)
		}
	}
	if e.AID == e.BID {
		errs = append(errs, errors.New("a_id and b_id must differ"))
	}
	if !e.Label.Valid() {
		errs = append(errs, fmt.Errorf("label %q is not a relationship type", truncate(string(e.Label), 64)))
	}
	if strings.TrimSpace(e.ConfirmedBy) == "" {
		errs = append(errs, errors.New("confirmed_by is required: only human-confirmed labels belong in labels.yaml"))
	}
	if _, err := time.Parse(DateLayout, e.ConfirmedOn); err != nil {
		errs = append(errs, fmt.Errorf("confirmed_on %q is not a %s date", truncate(e.ConfirmedOn, 64), DateLayout))
	}
	if len(e.Note) > MaxNoteLen {
		errs = append(errs, fmt.Errorf("note exceeds %d bytes", MaxNoteLen))
	}
	return errors.Join(errs...)
}

// Validate checks the schema version and every candidate.
func (c Candidates) Validate() error {
	var errs []error
	if c.Version != Version {
		errs = append(errs, fmt.Errorf("candidates version %d is not supported (want %d)", c.Version, Version))
	}
	if c.RoundID <= 0 {
		errs = append(errs, errors.New("round_id is required"))
	}
	if !c.Method.Valid() {
		errs = append(errs, fmt.Errorf("method %q is not valid", truncate(string(c.Method), 64)))
	}
	seen := map[string]int{}
	for i, e := range c.Candidates {
		for _, id := range []string{e.AID, e.BID} {
			if err := canonical.ValidateInstrumentID(id); err != nil {
				errs = append(errs, fmt.Errorf("candidates[%d]: %w", i, err))
			}
		}
		if e.AID == e.BID {
			errs = append(errs, fmt.Errorf("candidates[%d]: a_id and b_id must differ", i))
		}
		if !e.Suggested.Valid() {
			errs = append(errs, fmt.Errorf("candidates[%d]: suggested %q is not a relationship type", i, truncate(string(e.Suggested), 64)))
		}
		k := PairKey(e.AID, e.BID)
		if j, dup := seen[k]; dup {
			errs = append(errs, fmt.Errorf("candidates[%d]: duplicate of candidates[%d]", i, j))
		}
		seen[k] = i
	}
	return errors.Join(errs...)
}

// Suggestions maps each candidate pair to its rules-suggested type, for an
// offline eval (`equinox eval --candidates`).
func (c Candidates) Suggestions() map[string]canonical.RelationshipType {
	out := make(map[string]canonical.RelationshipType, len(c.Candidates))
	for _, e := range c.Candidates {
		out[PairKey(e.AID, e.BID)] = e.Suggested
	}
	return out
}

// LoadLabels reads and validates a labels file. A missing file is an error:
// the starter file ships with an empty list, so absence means a wrong path.
func LoadLabels(path string) (Labels, error) {
	var l Labels
	if err := decodeFile(path, &l); err != nil {
		return Labels{}, fmt.Errorf("loading labels %s: %w", path, err)
	}
	if err := l.Validate(); err != nil {
		return Labels{}, fmt.Errorf("loading labels %s: %w", path, err)
	}
	if l.Labels == nil {
		l.Labels = []Label{}
	}
	return l, nil
}

// LoadCandidates reads and validates a candidates file.
func LoadCandidates(path string) (Candidates, error) {
	var c Candidates
	if err := decodeFile(path, &c); err != nil {
		return Candidates{}, fmt.Errorf("loading candidates %s: %w", path, err)
	}
	if err := c.Validate(); err != nil {
		return Candidates{}, fmt.Errorf("loading candidates %s: %w", path, err)
	}
	return c, nil
}

// WriteCandidates writes c to path atomically (temp file + rename), creating
// the directory. It refuses any path whose basename is labels.yaml.
func WriteCandidates(path string, c Candidates) error {
	if filepath.Base(path) == LabelsFileName {
		return ErrLabelsPath
	}
	if err := c.Validate(); err != nil {
		return fmt.Errorf("writing candidates: %w", err)
	}
	body, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("encoding candidates: %w", err)
	}
	header := fmt.Sprintf("# Golden-set candidates drafted by `equinox golden draft` from round %d (rules only).\n"+
		"# MACHINE OUTPUT: `suggested` is the matcher's verdict, not a label. Confirm each\n"+
		"# pair's type in %s (the human-owned file; see its header). Re-running\n"+
		"# the draft overwrites this file and never touches %s.\n", c.RoundID, LabelsFileName, LabelsFileName)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("writing candidates: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".candidates-*.yaml")
	if err != nil {
		return fmt.Errorf("writing candidates: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.WriteString(header + string(body)); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("writing candidates: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("writing candidates: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("writing candidates: %w", err)
	}
	return nil
}

// decodeFile reads at most MaxFileBytes and decodes strict YAML into v.
func decodeFile(path string, v any) error {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
	if err != nil {
		return fmt.Errorf("reading: %w", err)
	}
	if len(raw) > MaxFileBytes {
		return fmt.Errorf("file exceeds %d bytes", MaxFileBytes)
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("file is empty")
		}
		return fmt.Errorf("decoding yaml: %w", err)
	}
	return nil
}

// truncate shortens external text for error messages.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
