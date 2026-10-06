package server

import (
	"errors"
	"io/fs"
	"net/http"
	"strings"
)

const msgNoClient = "client derlenmemiş: cd client && npm run build"

// csp is the Content-Security-Policy; extra adds connect-src sources.
func csp(extra []string) string {
	connect := strings.Join(append([]string{"'self'"}, extra...), " ")
	return "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data: blob:; " +
		"connect-src " + connect + "; font-src 'self'; object-src 'none'; base-uri 'none'; " +
		"form-action 'none'; frame-ancestors 'none'"
}

// secure sets the security headers on every response. HSTS is left to the
// TLS terminator (Cloudflare).
func secure(next http.Handler, policy string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", policy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), accelerometer=(self), gyroscope=(self)")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

func (s *Server[S, M, In, X]) index(w http.ResponseWriter, _ *http.Request) {
	if s.page == nil {
		http.Error(w, msgNoClient, http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(s.page)
}

// loadPage reads index.html once and fingerprints its bundle references;
// nil when the client is not built.
func loadPage(web fs.FS) []byte {
	b, err := fs.ReadFile(web, "index.html")
	if err != nil {
		return nil
	}
	return fingerprint(web, b)
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write([]byte("ok"))
}

// static serves files, never directory listings. Content-hashed URLs
// (?v=, from fingerprint) never change, so they are immutable; the bare
// fixed names must be revalidated; models/* may be cached a day, except the
// manifest that lists them.
func static(web fs.FS) http.Handler {
	files := http.FileServerFS(noDirs{web})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("v") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else if strings.HasPrefix(r.URL.Path, "/models/") && r.URL.Path != "/models/manifest.json" {
			w.Header().Set("Cache-Control", "public, max-age=86400")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
}

// noDirs hides directories, so the file server answers 404 instead of a
// listing (or an index.html redirect).
type noDirs struct{ fs.FS }

func (d noDirs) Open(name string) (fs.File, error) {
	f, err := d.FS.Open(name)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		f.Close()
		return nil, &fs.PathError{Op: "open", Path: name, Err: errors.Join(fs.ErrNotExist, err)}
	}
	return f, nil
}

type emptyFS struct{}

func (emptyFS) Open(string) (fs.File, error) { return nil, fs.ErrNotExist }
