package project

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	original := &Project{
		Project: ProjectMeta{
			Name:        "feastahead",
			Version:     "1.2.3.4",
			Description: "meal planning",
			Created:     "2026-01-15",
		},
		Stack: Stack{
			Languages:      []string{"typescript", "go"},
			Frameworks:     []string{"sveltekit", "drizzle"},
			PackageManager: "pnpm",
			TestRunner:     "vitest",
			Locked: []LockedChoice{
				{Choice: "sveltekit", Why: "ts ssr", LockedAt: "2026-01-15"},
			},
		},
		Runtime: Runtime{
			Mode:           "docker",
			DevCommand:     "docker compose up app",
			TestCommand:    "docker compose exec app pnpm test",
			MigrateCommand: "docker compose exec app pnpm db:migrate",
			Services: map[string]Service{
				"app": {URL: "http://localhost:5173", Health: "/api/health"},
				"db":  {URL: "postgres://localhost:5432/x", Admin: "psql"},
			},
		},
		Repo: Repo{
			Layout:           "single",
			GitMainBranch:    "main",
			BranchPattern:    "feature/*",
			CommitConvention: "conventional",
			SquashMerge:      true,
		},
		Paths: Paths{Source: "src", Tests: "src", Migrations: "src/db/migrations"},
		Env:   Env{Files: []string{".env", ".env.local"}, SecretsLocation: "1password", Gitignored: true},
		Goals: Goals{
			CoreValue: "meal planning that respects household constraints",
			NonGoals:  []string{"realtime collab"},
		},
		Constraints: map[string]string{"hosting": "self-hosted"},
		Competition: []Competitor{{Name: "mealime", URL: "https://mealime.com", WhatTheyDo: "X"}},
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
	if !reflect.DeepEqual(original, loaded) {
		t.Fatalf("round-trip mismatch:\noriginal: %+v\nloaded:   %+v", original, loaded)
	}
}

func TestLoadMissingFileReturnsError(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.toml")); err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestSaveDefaultsAreOmittedAndOptionalsRemainEmpty(t *testing.T) {
	// Empty project should serialise without explosion and load back equal.
	p := &Project{}
	dir := t.TempDir()
	path := filepath.Join(dir, "project.toml")
	if err := p.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Project.Name != "" {
		t.Errorf("expected empty name, got %q", loaded.Project.Name)
	}
	if loaded.Repo.SquashMerge {
		t.Error("expected SquashMerge to default false")
	}
}
