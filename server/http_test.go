package server_test

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ahmetbir/roomkit/server"
)

var webWithDirs = fstest.MapFS{
	"index.html":           {Data: []byte("<!doctype html>dogfight")},
	"app.js":               {Data: []byte("console.log(1)")},
	"style.css":            {Data: []byte("body{}")},
	"models/f16.glb":       {Data: []byte("glTF")},
	"models/manifest.json": {Data: []byte("[]")},
	"models/sub/x.glb":     {Data: []byte("glTF")},
	"empty/index.html":     {Data: []byte("nested index")},
}

func get(t *testing.T, url string) (int, string) {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

func head(t *testing.T, url string) *http.Response {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res
}

func TestSecurityHeadersEverywhere(t *testing.T) {
	srv, _ := newFake(t, server.Options{Web: webWithDirs}, 0)
	want := map[string]string{
		"Content-Security-Policy":    "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data: blob:; connect-src 'self'; font-src 'self'; object-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'",
		"X-Content-Type-Options":     "nosniff",
		"Referrer-Policy":            "no-referrer",
		"Permissions-Policy":         "camera=(), microphone=(), geolocation=(), accelerometer=(self), gyroscope=(self)",
		"Cross-Origin-Opener-Policy": "same-origin",
	}
	for _, path := range []string{"/", "/r/ABCD", "/app.js", "/healthz", "/missing", "/models/", "/ws"} {
		res := head(t, srv.URL+path)
		for k, v := range want {
			if got := res.Header.Get(k); got != v {
				t.Errorf("%s %s = %q", path, k, got)
			}
		}
		if res.Header.Get("Strict-Transport-Security") != "" {
			t.Errorf("%s sets HSTS (Cloudflare's job)", path)
		}
	}
}

func TestConnectSrcExtra(t *testing.T) {
	srv, _ := newFake(t, server.Options{ConnectSrc: []string{"wss://dogfight.example"}}, 0)
	if got := head(t, srv.URL+"/healthz").Header.Get("Content-Security-Policy"); !strings.Contains(got, "connect-src 'self' wss://dogfight.example;") {
		t.Fatal(got)
	}
}

func TestNoDirectoryListing(t *testing.T) {
	srv, _ := newFake(t, server.Options{Web: webWithDirs}, 0)
	for _, path := range []string{"/models/", "/models", "/models/sub/", "/empty/", "/empty"} {
		if res := head(t, srv.URL+path); res.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, res.StatusCode)
		}
	}
	if res := head(t, srv.URL+"/models/f16.glb"); res.StatusCode != 200 {
		t.Errorf("model file = %d", res.StatusCode)
	}
}

func TestCacheControl(t *testing.T) {
	srv, _ := newFake(t, server.Options{Web: webWithDirs}, 0)
	for path, want := range map[string]string{
		"/":                     "no-cache",
		"/r/ABCD":               "no-cache",
		"/app.js":               "no-cache",
		"/style.css":            "no-cache",
		"/models/f16.glb":       "public, max-age=86400",
		"/models/manifest.json": "no-cache", // lists the models: a redeploy must show at once
		"/healthz":              "no-store",
	} {
		if got := head(t, srv.URL+path).Header.Get("Cache-Control"); got != want {
			t.Errorf("%s Cache-Control = %q, want %q", path, got, want)
		}
	}
}

func TestHealthz(t *testing.T) {
	srv, _ := newFake(t, server.Options{}, 0)
	if code, body := get(t, srv.URL+"/healthz"); code != 200 || body != "ok" {
		t.Fatalf("healthz = %d %q", code, body)
	}
}
