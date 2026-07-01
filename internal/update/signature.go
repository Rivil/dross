package update

import (
	"errors"
	"fmt"

	"aead.dev/minisign"
)

// EmbeddedMinisignPublicKey is the production trust anchor: the base64 key line of
// the release-signing minisign public key (key id 664311AB85365746). This is the
// PUBLIC half — safe to commit and embed in the binary. Every release from this
// version onward signs checksums.txt with the matching private key, which lives
// only in CI secret storage.
const EmbeddedMinisignPublicKey = "RWRGVzaFqxFDZpeqfa4BbxCTnkoKCE2iyau5AB8C/aL+w10g41xlSbqi"

// TrustedMinisignKey is the public key VerifySignature is checked against in the
// update flow. It defaults to the embedded production key but is an exported
// override seam (same pattern as ghCommand / PRMergedFunc) so cmd-flow tests can
// swap in a throwaway public key generated in-test.
var TrustedMinisignKey = EmbeddedMinisignPublicKey

// Signature sentinels callers branch on, mirroring ErrChecksumMismatch.
var (
	// ErrSignatureMismatch is returned when a signature does not verify against the
	// trusted public key (tampered content or a wrong-key signature). The update
	// path MUST refuse on this.
	ErrSignatureMismatch = errors.New("signature verification failed")
	// ErrNoSignature is returned when no signature is present at all — a missing
	// .minisig is treated as tampering/misconfiguration, never a silent downgrade
	// to unsigned checksums (fail-closed).
	ErrNoSignature = errors.New("no signature")
)

// VerifySignature confirms that `signature` is a valid minisign signature over
// `signed` produced by the private key matching pubKeyText. pubKeyText is the
// base64 key line of a minisign public key (the "RW..." line of a minisign.pub).
//
// An empty signature returns ErrNoSignature (fail-closed on a missing .minisig).
// A malformed key returns a wrapped parse error. A signature that does not verify
// returns ErrSignatureMismatch. A valid signature returns nil. It handles both the
// legacy and the HASHED minisign signature framing.
func VerifySignature(signed, signature []byte, pubKeyText string) error {
	if len(signature) == 0 {
		return ErrNoSignature
	}
	var pub minisign.PublicKey
	if err := pub.UnmarshalText([]byte(pubKeyText)); err != nil {
		return fmt.Errorf("parse trusted minisign public key: %w", err)
	}
	if !minisign.Verify(pub, signed, signature) {
		return ErrSignatureMismatch
	}
	return nil
}
