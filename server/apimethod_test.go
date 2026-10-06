package server_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ahmetbir/roomkit/server"
)

// Spec §10.4: /api/* is GET (and HEAD) only.
func TestAPIMethods(t *testing.T) {
	srv, _ := newFake(t, server.Options{}, 0)
	for _, path := range []string{"/api/rooms", "/api/leaderboard?period=week", "/api/me"} {
		res, err := http.Post(srv.URL+path, "application/json", strings.NewReader("{}"))
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("POST %s = %d, want 405", path, res.StatusCode)
		}
		req, _ := http.NewRequest(http.MethodHead, srv.URL+path, nil)
		res, err = http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode == http.StatusMethodNotAllowed || len(body) != 0 {
			t.Errorf("HEAD %s = %d with %d body bytes, want an answer without a body", path, res.StatusCode, len(body))
		}
	}
}
