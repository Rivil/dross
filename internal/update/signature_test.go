package update

import (
	"crypto/rand"
	"errors"
	"testing"

	"aead.dev/minisign"
)

// genKey produces a throwaway minisign keypair for a self-contained test — no
// external minisign binary and no production private key are involved.
func genKey(t *testing.T) (minisign.PublicKey, minisign.PrivateKey) {
	t.Helper()
	pub, priv, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return pub, priv
}

func TestVerifySignatureValid(t *testing.T) {
	pub, priv := genKey(t)
	msg := []byte("deadbeef  dross_0.9.0_linux_amd64.tar.gz\n")
	sig := minisign.Sign(priv, msg)

	if err := VerifySignature(msg, sig, pub.String()); err != nil {
		t.Fatalf("valid signature: want nil, got %v", err)
	}
}

func TestVerifySignatureWrongKey(t *testing.T) {
	_, priv := genKey(t)
	otherPub, _ := genKey(t)
	msg := []byte("deadbeef  dross_0.9.0_linux_amd64.tar.gz\n")
	sig := minisign.Sign(priv, msg)

	err := VerifySignature(msg, sig, otherPub.String())
	if !errors.Is(err, ErrSignatureMismatch) {
		t.Fatalf("wrong key: want ErrSignatureMismatch, got %v", err)
	}
}

func TestVerifySignatureTamperedContent(t *testing.T) {
	pub, priv := genKey(t)
	msg := []byte("deadbeef  dross_0.9.0_linux_amd64.tar.gz\n")
	sig := minisign.Sign(priv, msg)

	tampered := make([]byte, len(msg))
	copy(tampered, msg)
	tampered[0] ^= 0x01 // flip one byte

	err := VerifySignature(tampered, sig, pub.String())
	if !errors.Is(err, ErrSignatureMismatch) {
		t.Fatalf("tampered content: want ErrSignatureMismatch, got %v", err)
	}
}

func TestVerifySignatureEmpty(t *testing.T) {
	pub, _ := genKey(t)
	msg := []byte("deadbeef  dross_0.9.0_linux_amd64.tar.gz\n")

	if err := VerifySignature(msg, nil, pub.String()); !errors.Is(err, ErrNoSignature) {
		t.Fatalf("nil signature: want ErrNoSignature, got %v", err)
	}
	if err := VerifySignature(msg, []byte{}, pub.String()); !errors.Is(err, ErrNoSignature) {
		t.Fatalf("empty signature: want ErrNoSignature, got %v", err)
	}
}
