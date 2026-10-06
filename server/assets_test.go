package server

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"testing/fstest"
)

var fingerprintWeb = fstest.MapFS{
	"index.html": {Data: []byte(`<link rel="stylesheet" href="/style.css" /><link rel="stylesheet" href="/hud.css" /><link rel="stylesheet" href="/mobile.css" /><script type="module" src="/app.js"></script>`)},
	"app.js":     {Data: []byte("console.log(1)")},
	"style.css":  {Data: []byte("body{}")},
	"hud.css":    {Data: []byte(".h{}")},
	"mobile.css": {Data: []byte(".m{}")},
}

func version(data string) string {
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:6])
}

func TestIndexReferencesContentHashedBundle(t *testing.T) {
	srv := newServer(t, Options{Web: fingerprintWeb})
	for _, path := range []string{"/", "/r/ABCD"} {
		_, body := get(t, srv.URL+path)
		for _, want := range []string{
			`src="/app.js?v=` + version("console.log(1)") + `"`,
			`href="/style.css?v=` + version("body{}") + `"`,
			`href="/hud.css?v=` + version(".h{}") + `"`,
			`href="/mobile.css?v=` + version(".m{}") + `"`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("GET %s: %q lacks %s", path, body, want)
			}
		}
	}
}

func TestVersionedAssetsAreImmutable(t *testing.T) {
	srv := newServer(t, Options{Web: fingerprintWeb})
	if got := head(t, srv.URL+"/app.js?v=abc").Header.Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("versioned app.js Cache-Control = %q", got)
	}
	if got := head(t, srv.URL+"/app.js").Header.Get("Cache-Control"); got != "no-cache" {
		t.Errorf("bare app.js Cache-Control = %q", got)
	}
	if got := head(t, srv.URL+"/").Header.Get("Cache-Control"); got != "no-cache" {
		t.Errorf("index Cache-Control = %q", got)
	}
}
