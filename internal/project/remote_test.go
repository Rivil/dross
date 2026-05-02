package project

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestRemoteRoundTrip(t *testing.T) {
	original := &Project{
		Remote: Remote{
			URL:       "https://forge.example/me/proj",
			Provider:  "forgejo",
			Public:    false,
			APIBase:   "https://forge.example/api/v1",
			LogAPI:    true,
			AuthEnv:   "FORGEJO_TOKEN",
			Reviewers: []string{"alice", "bob"},
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
	if !reflect.DeepEqual(original.Remote, loaded.Remote) {
		t.Fatalf("Remote round-trip mismatch:\norig:   %+v\nloaded: %+v", original.Remote, loaded.Remote)
	}
}

func TestDetectRemote(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		wantURL        string
		wantProvider   string
		wantPublic     bool
		wantAPIBase    string
	}{
		{
			name:         "github https",
			input:        "https://github.com/Rivil/dross",
			wantURL:      "https://github.com/Rivil/dross",
			wantProvider: "github",
			wantPublic:   true,
			wantAPIBase:  "https://api.github.com",
		},
		{
			name:         "github https with .git suffix",
			input:        "https://github.com/Rivil/dross.git",
			wantURL:      "https://github.com/Rivil/dross",
			wantProvider: "github",
			wantPublic:   true,
			wantAPIBase:  "https://api.github.com",
		},
		{
			name:         "github ssh scp form",
			input:        "git@github.com:Rivil/dross.git",
			wantURL:      "https://github.com/Rivil/dross",
			wantProvider: "github",
			wantPublic:   true,
			wantAPIBase:  "https://api.github.com",
		},
		{
			name:         "github ssh url form",
			input:        "ssh://git@github.com/Rivil/dross.git",
			wantURL:      "https://github.com/Rivil/dross",
			wantProvider: "github",
			wantPublic:   true,
			wantAPIBase:  "https://api.github.com",
		},
		{
			name:         "codeberg → forgejo",
			input:        "https://codeberg.org/me/proj",
			wantURL:      "https://codeberg.org/me/proj",
			wantProvider: "forgejo",
			wantPublic:   true,
			wantAPIBase:  "https://codeberg.org/api/v1",
		},
		{
			name:         "bitbucket",
			input:        "git@bitbucket.org:me/proj.git",
			wantURL:      "https://bitbucket.org/me/proj",
			wantProvider: "bitbucket",
			wantPublic:   true,
			wantAPIBase:  "https://api.bitbucket.org/2.0",
		},
		{
			name:         "self-hosted unknown host — URL filled, provider empty",
			input:        "https://forge.example.internal/me/proj.git",
			wantURL:      "https://forge.example.internal/me/proj",
			wantProvider: "",
			wantPublic:   false,
			wantAPIBase:  "",
		},
		{
			name:         "self-hosted ssh — URL filled, provider empty",
			input:        "git@forge.example.internal:me/proj.git",
			wantURL:      "https://forge.example.internal/me/proj",
			wantProvider: "",
			wantPublic:   false,
			wantAPIBase:  "",
		},
		{
			name:         "empty string returns zero value",
			input:        "",
			wantURL:      "",
			wantProvider: "",
			wantPublic:   false,
			wantAPIBase:  "",
		},
		{
			name:         "garbage returns zero value",
			input:        "not-a-url",
			wantURL:      "",
			wantProvider: "",
			wantPublic:   false,
			wantAPIBase:  "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DetectRemote(tc.input)
			if got.URL != tc.wantURL {
				t.Errorf("URL: got %q, want %q", got.URL, tc.wantURL)
			}
			if got.Provider != tc.wantProvider {
				t.Errorf("Provider: got %q, want %q", got.Provider, tc.wantProvider)
			}
			if got.Public != tc.wantPublic {
				t.Errorf("Public: got %v, want %v", got.Public, tc.wantPublic)
			}
			if got.APIBase != tc.wantAPIBase {
				t.Errorf("APIBase: got %q, want %q", got.APIBase, tc.wantAPIBase)
			}
		})
	}
}
