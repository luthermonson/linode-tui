package onepassword

import (
	"context"
	"errors"
	"testing"
)

// These tests exercise ref validation only — they must pass whether or not
// the op binary is actually installed, since validation happens before any
// exec.LookPath/exec.Command call.

func TestReadRejectsNonOpRef(t *testing.T) {
	cases := []string{
		"",
		"not-a-ref",
		"http://evil.example/steal",
		"-x",               // flag-injection shaped
		"--help",           // flag-injection shaped
		"op:/Work/thing",   // malformed scheme (single slash)
		" op://Work/thing", // leading whitespace
	}
	for _, ref := range cases {
		_, err := Read(context.Background(), ref)
		if !errors.Is(err, ErrInvalidRef) {
			t.Errorf("ref %q: expected ErrInvalidRef, got %v", ref, err)
		}
	}
}

func TestReadAcceptsOpRefPrefixShape(t *testing.T) {
	// A well-formed op:// ref should pass validation and proceed to actually
	// try to resolve it. We don't require the op binary to be installed in
	// this test environment, so we only assert that the failure (if any)
	// is NOT ErrInvalidRef — i.e. validation let it through.
	_, err := Read(context.Background(), "op://Work/linode-dev/credential")
	if errors.Is(err, ErrInvalidRef) {
		t.Fatalf("well-formed op:// ref should not be rejected by validation, got %v", err)
	}
}
