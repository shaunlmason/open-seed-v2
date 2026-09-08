package mirror

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
)

// snapshot is the file-backed reference exporter: a JSON file of
// issues, so a mirror can be planned and applied with no network and
// the suite has an arm that holds the same contract as the forges. It
// is also what a deployment without a forge can point at.
type snapshot struct {
	path string
}

func newSnapshot(cfg Config) (Adapter, error) {
	if cfg.Snapshot == "" {
		return nil, errors.New("the snapshot exporter needs --snapshot <file>")
	}
	return &snapshot{path: cfg.Snapshot}, nil
}

func (s *snapshot) Name() string { return "snapshot" }

type snapshotFile struct {
	Next   int     `json:"next"`
	Issues []Issue `json:"issues"`
}

func (s *snapshot) read() (snapshotFile, error) {
	var f snapshotFile
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return snapshotFile{Next: 1, Issues: []Issue{}}, nil
	}
	if err != nil {
		return f, err
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return f, fmt.Errorf("the snapshot does not parse: %v", err)
	}
	if f.Next < 1 {
		f.Next = 1
	}
	if f.Issues == nil {
		f.Issues = []Issue{}
	}
	return f, nil
}

func (s *snapshot) write(f snapshotFile) error {
	sort.Slice(f.Issues, func(i, j int) bool { return f.Issues[i].ID < f.Issues[j].ID })
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, append(b, '\n'), 0o644)
}

func (s *snapshot) List(context.Context) ([]Issue, error) {
	f, err := s.read()
	if err != nil {
		return nil, err
	}
	return f.Issues, nil
}

func (s *snapshot) Create(_ context.Context, d Desired) (Issue, error) {
	f, err := s.read()
	if err != nil {
		return Issue{}, err
	}
	is := Issue{ID: strconv.Itoa(f.Next), Title: d.Title, Body: d.Body, Labels: []string{d.Label}, Closed: d.Closed}
	f.Next++
	f.Issues = append(f.Issues, is)
	return is, s.write(f)
}

func (s *snapshot) Update(_ context.Context, id string, d Desired, foreign []string) error {
	f, err := s.read()
	if err != nil {
		return err
	}
	for i := range f.Issues {
		if f.Issues[i].ID != id {
			continue
		}
		f.Issues[i].Title, f.Issues[i].Body, f.Issues[i].Closed = d.Title, d.Body, d.Closed
		f.Issues[i].Labels = append(append([]string{}, foreign...), d.Label)
		return s.write(f)
	}
	return fmt.Errorf("no issue %s in the snapshot", id)
}
