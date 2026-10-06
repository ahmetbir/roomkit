package pilot

import (
	"strings"
	"testing"
)

func TestNewIsValidAndUnique(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		tok := New()
		if !Valid(tok) || len(tok) != TokenLen || seen[tok] {
			t.Fatalf("bad or repeated token %q", tok)
		}
		seen[tok] = true
	}
}

func TestValidRejects(t *testing.T) {
	for _, bad := range []string{"", "short", strings.Repeat("A", 21), strings.Repeat("A", 23), strings.Repeat("A", 21) + "=",
		strings.Repeat("A", 21) + "+", strings.Repeat("A", 21) + "/", strings.Repeat("ş", 11), strings.Repeat("A", 10000)} {
		if Valid(bad) {
			t.Fatalf("accepted %q", bad)
		}
	}
	if !Valid("AAAAAAAAAAAAAAAAAAAAAA") || !Valid("abc_DEF-0123456789xyzQ") {
		t.Fatal("valid tokens rejected")
	}
}

func TestHashIsHexSHA256(t *testing.T) {
	h := Hash("AAAAAAAAAAAAAAAAAAAAAA")
	if len(h) != 64 || strings.Trim(h, "0123456789abcdef") != "" || h == Hash("AAAAAAAAAAAAAAAAAAAAAB") {
		t.Fatalf("hash %q", h)
	}
}

func TestValidIsStrict(t *testing.T) {
	// The last char carries 2 data bits and 4 padding bits; only canonical
	// encodings (padding bits zero) are accepted, so 16 bytes have one token.
	for _, bad := range []string{"AAAAAAAAAAAAAAAAAAAAAB", "AAAAAAAAAAAAAAAAAAAAAP", "AAAAAAAAAAAAAAAAAAAAA_"} {
		if Valid(bad) {
			t.Fatalf("accepted non-canonical %q", bad)
		}
	}
	for _, ok := range []string{"AAAAAAAAAAAAAAAAAAAAAQ", "AAAAAAAAAAAAAAAAAAAAAg", "AAAAAAAAAAAAAAAAAAAAAw"} {
		if !Valid(ok) {
			t.Fatalf("rejected canonical %q", ok)
		}
	}
}
