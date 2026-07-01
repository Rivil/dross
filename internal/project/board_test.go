package project

import (
	"path/filepath"
	"reflect"
	"testing"
)

// TestProjectBoardRoundTrip guards the [board] block: every field — including
// the state_map map — must survive Save→Load unchanged. A dropped field or a
// wrong toml tag breaks the DeepEqual.
func TestProjectBoardRoundTrip(t *testing.T) {
	original := &Project{
		Project: ProjectMeta{Name: "dross", Version: "0.6.12.0", Created: "2026-06-29"},
		Board: Board{
			Provider:      "youtrack",
			BaseURL:       "https://yt.example.com",
			AuthEnv:       "YOUTRACK_TOKEN",
			AuthUser:      "me@example.com",
			Project:       "PROJ",
			GitHubProject: "PVT_kwDOABCD1234",
			Enabled:       true,
			MilestoneMode: "version",
			StateMap:      map[string]string{"shipped": "Fixed"},
		},
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "project.toml")

	if err := original.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !reflect.DeepEqual(original.Board, loaded.Board) {
		t.Fatalf("board round-trip mismatch:\noriginal: %+v\nloaded:   %+v", original.Board, loaded.Board)
	}
}
