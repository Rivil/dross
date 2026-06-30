package statusline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// statusInput is the subset of Claude Code's stdin status JSON the status line uses.
type statusInput struct {
	Model struct {
		DisplayName string `json:"display_name"`
	} `json:"model"`
	Workspace struct {
		CurrentDir string `json:"current_dir"`
	} `json:"workspace"`
	SessionID     string `json:"session_id"`
	ContextWindow *struct {
		RemainingPercentage *float64 `json:"remaining_percentage"`
		TotalTokens         int      `json:"total_tokens"`
	} `json:"context_window"`
}

// Gather resolves the render Inputs from the stdin status JSON plus the filesystem,
// reading every external dependency through an injected seam: env(name) for
// environment variables (CLAUDE_CONFIG_DIR / HOME / CLAUDE_JOB_DIR /
// CLAUDE_CODE_AUTO_COMPACT_WINDOW), now for the peer-staleness clock, and
// gitBranch(dir) for the branch. This keeps Gather a deterministic function of its
// inputs so golden and command tests are hermetic. Only a stdin JSON parse failure
// returns an error; filesystem problems (missing todos/jobs/state) degrade to empty
// fields exactly as the reference statusline silently does.
func Gather(stdin []byte, env func(string) string, now time.Time, gitBranch func(dir string) string) (Inputs, error) {
	var data statusInput
	if err := json.Unmarshal(stdin, &data); err != nil {
		return Inputs{}, err
	}

	model := data.Model.DisplayName
	if model == "" {
		model = "Claude"
	}
	dir := data.Workspace.CurrentDir
	if dir == "" {
		if wd, err := os.Getwd(); err == nil {
			dir = wd
		}
	}

	home := env("HOME")
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	claudeDir := env("CLAUDE_CONFIG_DIR")
	if claudeDir == "" {
		claudeDir = filepath.Join(home, ".claude")
	}

	// Todo wins over dross state: only resolve the state when there is no todo.
	task := readInProgressTodo(filepath.Join(claudeDir, "todos"), data.SessionID)
	drossState := ""
	if task == "" {
		drossState = formatDrossState(readDrossState(dir, home))
	}

	currentJobID := ""
	if jd := env("CLAUDE_JOB_DIR"); jd != "" {
		currentJobID = filepath.Base(jd)
	}
	peers := readPeerJobs(filepath.Join(claudeDir, "jobs"), currentJobID, now)

	acw := 0
	if v := strings.TrimSpace(env("CLAUDE_CODE_AUTO_COMPACT_WINDOW")); v != "" {
		acw, _ = strconv.Atoi(v) // non-numeric => 0 => render uses the default buffer
	}

	var remaining *float64
	totalTokens := 0
	if data.ContextWindow != nil {
		remaining = data.ContextWindow.RemainingPercentage
		totalTokens = data.ContextWindow.TotalTokens
	}

	return Inputs{
		Model:             model,
		Dir:               dir,
		Branch:            gitBranch(dir),
		TodoActiveForm:    task,
		DrossState:        drossState,
		RemainingPercent:  remaining,
		TotalTokens:       totalTokens,
		AutoCompactWindow: acw,
		Peers:             peers,
	}, nil
}

// readInProgressTodo returns the activeForm of the first in-progress todo in the
// newest-by-mtime <session>*-agent-*.json file under todosDir, or "". Only the
// single newest matching file is consulted (matching the reference).
func readInProgressTodo(todosDir, session string) string {
	if session == "" {
		return ""
	}
	entries, err := os.ReadDir(todosDir)
	if err != nil {
		return ""
	}
	type cand struct {
		path  string
		mtime time.Time
	}
	var cands []cand
	for _, e := range entries {
		n := e.Name()
		if !strings.HasPrefix(n, session) || !strings.Contains(n, "-agent-") || !strings.HasSuffix(n, ".json") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		cands = append(cands, cand{filepath.Join(todosDir, n), info.ModTime()})
	}
	if len(cands) == 0 {
		return ""
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].mtime.After(cands[j].mtime) })

	data, err := os.ReadFile(cands[0].path)
	if err != nil {
		return ""
	}
	var todos []struct {
		Status     string `json:"status"`
		ActiveForm string `json:"activeForm"`
	}
	if err := json.Unmarshal(data, &todos); err != nil {
		return ""
	}
	for _, td := range todos {
		if td.Status == "in_progress" {
			return td.ActiveForm
		}
	}
	return ""
}

// readDrossState walks up from dir (<=10 levels, stopping at the home boundary or
// filesystem root) looking for .dross/state.json, returning the parsed object or nil.
func readDrossState(dir, home string) map[string]any {
	current := dir
	for i := 0; i < 10; i++ {
		candidate := filepath.Join(current, ".dross", "state.json")
		if data, err := os.ReadFile(candidate); err == nil {
			var s map[string]any
			if err := json.Unmarshal(data, &s); err != nil {
				return nil
			}
			return s
		}
		parent := filepath.Dir(current)
		if parent == current || current == home {
			break
		}
		current = parent
	}
	return nil
}

// formatDrossState renders "milestone · phase · status" from the state object,
// skipping fields that are absent or empty (so a partial state degrades gracefully).
func formatDrossState(s map[string]any) string {
	if s == nil {
		return ""
	}
	var parts []string
	for _, k := range []string{"current_milestone", "current_phase", "current_phase_status"} {
		if v, ok := s[k].(string); ok && v != "" {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, " · ")
}

// readPeerJobs reads sibling jobs from jobsDir/<id>/state.json, skipping the current
// job (currentJobID) and any job whose updatedAt is older than 6h relative to now.
// Unreadable / non-object state files are skipped, never fatal.
func readPeerJobs(jobsDir, currentJobID string, now time.Time) []Peer {
	entries, err := os.ReadDir(jobsDir)
	if err != nil {
		return nil
	}
	const maxAge = 6 * time.Hour
	var peers []Peer
	for _, e := range entries {
		id := e.Name()
		if id == currentJobID {
			continue
		}
		data, err := os.ReadFile(filepath.Join(jobsDir, id, "state.json"))
		if err != nil {
			continue
		}
		var s struct {
			Name        string `json:"name"`
			DaemonShort string `json:"daemonShort"`
			State       string `json:"state"`
			Detail      string `json:"detail"`
			UpdatedAt   string `json:"updatedAt"`
		}
		if err := json.Unmarshal(data, &s); err != nil {
			continue
		}
		if s.UpdatedAt != "" {
			if t, err := time.Parse(time.RFC3339, s.UpdatedAt); err == nil && now.Sub(t) > maxAge {
				continue
			}
		}
		name := s.Name
		if name == "" {
			name = s.DaemonShort
		}
		if name == "" {
			name = id
		}
		peers = append(peers, Peer{Name: name, State: s.State, Detail: s.Detail})
	}
	return peers
}
