package project

import (
	"encoding/json"
	"fmt"

	"github.com/shaunlmason/open-seed-v2/internal/boundary"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

// Boundary is the cross-organization task view (plans/os-40ed0ca0.md
// D2; spec/boundary.md): one file per cross-repo request under
// tasks/<request>.json carrying the pinned fields alone — the
// request's position, its answer's, the five-state lifecycle derived
// from the target chain, and the artifact digests the contract
// published — plus tasks.json, the index. A chain carrying no
// cross-repo request builds the index empty and nothing else, so no
// other projection moves for it.
func Boundary() Projection {
	return Projection{Name: "boundary", Version: "1", Build: buildBoundary}
}

func buildBoundary(records []*event.Record, _ Inputs) (map[string][]byte, error) {
	table, err := transition.Default()
	if err != nil {
		return nil, err
	}
	tasks := boundary.Tasks(records, table.FoldRecords(records))
	files := map[string][]byte{}
	index, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return nil, err
	}
	files["tasks.json"] = append(index, '\n')
	for _, t := range tasks {
		b, err := json.MarshalIndent(t, "", "  ")
		if err != nil {
			return nil, err
		}
		files[fmt.Sprintf("tasks/%d.json", t.Request)] = append(b, '\n')
	}
	return files, nil
}
