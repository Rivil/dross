package stack

// ResolvedRuntime is the runtime command set a profile contributes to
// project.toml [runtime]. Empty slots mean the profile declares no command there.
type ResolvedRuntime struct {
	Test      string
	Typecheck string
	Format    string
	Build     string
}

// ResolveRuntime resolves all four runtime slots of a profile for the given GOOS,
// using lookPath to gate command variants by binary availability. Pass
// exec.LookPath in production; inject a fake in tests.
func ResolveRuntime(p *Profile, goos string, lookPath func(string) (string, error)) ResolvedRuntime {
	return ResolvedRuntime{
		Test:      ResolveCommand(p.Runtime.Test, goos, lookPath),
		Typecheck: ResolveCommand(p.Runtime.Typecheck, goos, lookPath),
		Format:    ResolveCommand(p.Runtime.Format, goos, lookPath),
		Build:     ResolveCommand(p.Runtime.Build, goos, lookPath),
	}
}

// ResolveCommand reduces a single runtime slot to one command line.
//
// A slot with explicit Variants resolves to the first OS-matching variant whose
// gate binary is available on PATH; if none are available it falls back to the
// first OS-matching variant's command (so a slot is never silently empty just
// because the preferred tool is missing). A slot with no variants uses its Run
// shorthand. The result is always exactly one variant's command — never a
// concatenation of several.
func ResolveCommand(c Command, goos string, lookPath func(string) (string, error)) string {
	if len(c.Variants) == 0 {
		return c.Run
	}
	var fallback string
	haveFallback := false
	for _, v := range c.Variants {
		if v.OS != "" && v.OS != goos {
			continue
		}
		if !haveFallback {
			fallback = v.Run
			haveFallback = true
		}
		if v.Bin == "" {
			return v.Run // ungated variant is always usable
		}
		if _, err := lookPath(v.Bin); err == nil {
			return v.Run
		}
	}
	return fallback
}
