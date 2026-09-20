package cmd

import "github.com/Rivil/dross/internal/consent"

// Test-only shims over internal/consent with the root-taking signatures cmd's
// tests were written against, so cmd_test.go, consent_surface_test.go,
// doctor_lane_toolchain_test.go, survivor_drain_consent_test.go,
// test_files_test.go, redproof_replay_test.go, redproof_repoint_cmd_test.go
// and execconsent_audit_test.go keep driving the SAME store production uses
// (grantStore) without an assertion edit. Production callers name the
// package; none of these exist outside `go test`.

func Fingerprint(command string) string { return consent.Fingerprint(command) }

func CheckConsent(root, repoDir, testCmd string) (ConsentState, error) {
	return consent.CheckConsent(grantStore(root), repoDir, testCmd)
}

func GrantConsent(root, testCmd string) error {
	return consent.GrantConsent(grantStore(root), testCmd)
}

func ReplayConsented(root, line string) (bool, error) {
	return consent.ReplayConsented(grantStore(root), line)
}

func GrantReplayConsent(root, line string) error {
	return consent.GrantReplayConsent(grantStore(root), line)
}

func RunConsented(root, line string) (bool, error) {
	return consent.RunConsented(grantStore(root), line)
}

func GrantRunConsent(root, line string) error {
	return consent.GrantRunConsent(grantStore(root), line)
}
