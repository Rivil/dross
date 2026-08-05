package ship

import (
	"fmt"
	"os"

	"github.com/Rivil/dross/internal/hostallow"
)

// This file is the ONLY place in internal/ship that reads a token out of the
// environment.
//
// That is a structural claim, not a convention: TestNoGetenvOutsideHostguard
// greps the package's non-test sources for `os.Getenv(` and fails, by file and
// line, on any occurrence outside this file. The check is deliberately on the
// bare call rather than on `opts.AuthEnv`, because bitbucket.go reads through a
// local variable and a narrower pattern would have missed it — which is exactly
// how the next one gets missed too.
//
// The reason it has to be structural: five separate call sites across four
// backends each did `os.Getenv(opts.AuthEnv)` inline, and a guard added to four
// of them is a guard with a hole. Funnelling the read through one function makes
// "did this backend check the host?" answerable by construction instead of by
// review.

// resolveToken checks apiBase against the allowlist and only then reads the
// token named by authEnv.
//
// The order is the guarantee. Once os.Getenv has run, the secret is a string in
// this process, one careless %v away from a log line or an error message; the
// point of the check is that a refused host never causes it to be read at all.
// Both errors are returned before any socket is opened.
func resolveToken(apiBase, authEnv string, policy hostallow.Policy) (string, error) {
	if err := policy.Check("[remote].api_base", apiBase); err != nil {
		return "", err
	}
	token := os.Getenv(authEnv)
	if token == "" {
		return "", fmt.Errorf("$%s is not set; run `dross env set %s` in your shell", authEnv, authEnv)
	}
	return token, nil
}
