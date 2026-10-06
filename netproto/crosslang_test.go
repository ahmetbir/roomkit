package netproto

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ahmetbir/roomkit/internal/tsconst"
)

// The client core lists the same error codes, in the same order, and
// translates each (ts/net/codes.ts).
func TestErrorCodesMatchClientCore(t *testing.T) {
	got, err := tsconst.Strings("net/codes.ts", "ERROR_CODES")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, ErrorCodes()) {
		t.Fatalf("ERROR_CODES: client %v, server %v", got, ErrorCodes())
	}
}

// The welcome envelope has the same fields on both sides
// (ts/net/socket.ts Welcome).
func TestWelcomeMatchesClientCore(t *testing.T) {
	got, err := tsconst.Fields("net/socket.ts", "Welcome")
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for f := range reflect.TypeFor[Welcome]().Fields() {
		want = append(want, strings.Split(f.Tag.Get("json"), ",")[0])
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Welcome: client %v, server %v", got, want)
	}
}
