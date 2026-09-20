package cmd

import (
	"github.com/Rivil/dross/internal/consent"
	"github.com/Rivil/dross/internal/project"
)

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

// --- lane shims (t-5) ---

func laneConsentLine(lane project.TestLane) string        { return consent.LaneLine(lane) }
func laneInstallConsentLine(lane project.TestLane) string { return consent.LaneInstallLine(lane) }

func LaneConsented(root, repoDir, name, line string) (ConsentState, error) {
	return consent.LaneConsented(grantStore(root), repoDir, name, line)
}

func GrantLaneConsent(root, name, line string) error {
	return consent.GrantLaneConsent(grantStore(root), name, line)
}

func RevokeLaneConsent(root, name string) error {
	return consent.RevokeLaneConsent(grantStore(root), name)
}

func LaneInstallConsented(root, repoDir, name, line string) (ConsentState, error) {
	return consent.LaneInstallConsented(grantStore(root), repoDir, name, line)
}

func GrantLaneInstallConsent(root, name, line string) error {
	return consent.GrantLaneInstallConsent(grantStore(root), name, line)
}

func RevokeLaneInstallConsent(root, name string) error {
	return consent.RevokeLaneInstallConsent(grantStore(root), name)
}

const (
	laneFrame         = consent.LaneFrame
	laneTemplateFrame = consent.LaneTemplateFrame
)

func laneInstallRefusal(lane project.TestLane, state ConsentState, cerr error) error {
	return consent.LaneInstallRefusal(lane, state, cerr)
}
