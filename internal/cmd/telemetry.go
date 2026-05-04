package cmd

import (
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/defaults"
	"github.com/Rivil/dross/internal/telemetry"
)

// RecordCLIEvent writes a "cli" telemetry event for one dross
// invocation. Called from main() after root.Execute. Errors are
// swallowed: telemetry must never break the user's workflow.
//
// Honors:
//   - DROSS_NO_TELEMETRY env var (handled inside telemetry.Append)
//   - defaults.toml [telemetry] enabled = false
//
// Recording is best-effort. If defaults can't be read, we still try to
// record (env var is the authoritative kill-switch).
func RecordCLIEvent(c *cobra.Command, dur time.Duration, runErr error) {
	if !telemetryEnabled() {
		return
	}
	cmdPath := ""
	if c != nil {
		cmdPath = c.CommandPath()
	}
	repoHash := ""
	if root, err := FindRoot(); err == nil {
		// FindRoot returns the .dross dir; hash its parent (repo root).
		repoHash = telemetry.HashRepo(filepath.Dir(root))
	}

	exit := 0
	errClass := ""
	if runErr != nil {
		exit = 1
		errClass = telemetry.ClassifyError(runErr)
	}

	_ = telemetry.Append(telemetryPath(), telemetry.Event{
		Kind:       "cli",
		Command:    cmdPath,
		DurationMS: dur.Milliseconds(),
		ExitCode:   exit,
		ErrorClass: errClass,
		RepoHash:   repoHash,
	})
}

// RecordOutcomeEvent writes a "outcome" event from an in-flight command
// (verify, ship, phase create, etc). Use Counts/Numbers/Tags to capture
// shape without leaking content. Same swallow-on-error guarantee.
func RecordOutcomeEvent(name string, counts map[string]int, numbers map[string]float64, tags map[string]string) {
	if !telemetryEnabled() {
		return
	}
	repoHash := ""
	if root, err := FindRoot(); err == nil {
		repoHash = telemetry.HashRepo(filepath.Dir(root))
	}
	_ = telemetry.Append(telemetryPath(), telemetry.Event{
		Kind:     "outcome",
		Command:  name,
		Counts:   counts,
		Numbers:  numbers,
		Tags:     tags,
		RepoHash: repoHash,
	})
}

// telemetryPath returns ~/.claude/dross/telemetry.jsonl. Returns the
// empty string if the home dir can't be resolved — Append handles
// empty paths defensively.
func telemetryPath() string {
	dir, err := GlobalDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, telemetry.File)
}

// telemetryEnabled reads ~/.claude/dross/defaults.toml's [telemetry]
// block. Defaults to true (per the default-ON policy). Read errors are
// treated as "enabled" so a corrupt config doesn't silently kill
// telemetry.
func telemetryEnabled() bool {
	dir, err := GlobalDir()
	if err != nil {
		return true
	}
	d, err := defaults.LoadFile(filepath.Join(dir, defaults.File))
	if err != nil {
		return true
	}
	return d.Telemetry.TelemetryEnabled()
}
