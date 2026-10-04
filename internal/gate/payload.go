package gate

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
)

// Payload is the JSON Claude Code writes to a hook's stdin.
//
// It is decoded key by key rather than into a tagged struct. The shape is
// Claude Code's, not dross's; tool_input and tool_response differ per tool;
// and a key this version does not know must cost nothing. The paths it
// carries — cwd and a file tool's file_path — are absolute host locations the
// engine only walks lexically, and opens only through pathfence.Contain
// against the root it locates (see LocateRoot and ContainIn).
type Payload struct {
	Event     string
	SessionID string
	ToolName  string
	// Cwd is the directory the tool call runs in.
	Cwd string
	// Input is tool_input as decoded JSON; nil when absent or not an object.
	Input map[string]any
	// Response is tool_response, undecoded: only PostToolUse carries it, and
	// its shape is the tool's own.
	Response json.RawMessage
}

// Decode reads a hook payload. Anything that is not a JSON object naming a
// tool is an error the caller turns into a warning, never a block: refusing
// on an unreadable payload would stop every tool call the day Claude Code
// changes its hook schema.
func Decode(b []byte) (Payload, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(b, &top); err != nil {
		return Payload{}, fmt.Errorf("unparseable hook payload: %w", err)
	}
	if top == nil {
		return Payload{}, errors.New("unparseable hook payload: not a JSON object")
	}
	p := Payload{
		Event:     str(top, "hook_event_name"),
		SessionID: str(top, "session_id"),
		ToolName:  str(top, "tool_name"),
		Cwd:       str(top, "cwd"),
		Response:  top["tool_response"],
	}
	if p.ToolName == "" {
		return Payload{}, errors.New("hook payload names no tool_name")
	}
	if raw, ok := top["tool_input"]; ok {
		_ = json.Unmarshal(raw, &p.Input) // a non-object input reads as none
	}
	return p, nil
}

func str(top map[string]json.RawMessage, key string) string {
	var s string
	if raw, ok := top[key]; ok {
		_ = json.Unmarshal(raw, &s)
	}
	return s
}

// Field is a string-valued key of tool_input, "" when absent or not a string.
func (p Payload) Field(key string) string {
	s, _ := p.Input[key].(string)
	return s
}

// Command is a Bash call's command line.
func (p Payload) Command() string {
	if p.ToolName != "Bash" {
		return ""
	}
	return p.Field("command")
}

// FilePath is the file a file tool targets — file_path, or a NotebookEdit's
// notebook_path — made absolute against Cwd. "" for any other tool.
func (p Payload) FilePath() string {
	f := p.Field("file_path")
	if f == "" {
		f = p.Field("notebook_path")
	}
	if f == "" {
		return ""
	}
	if !filepath.IsAbs(f) && p.Cwd != "" {
		f = filepath.Join(p.Cwd, f)
	}
	return filepath.Clean(f)
}
