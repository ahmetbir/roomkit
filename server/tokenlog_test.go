package server

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The raw pilot token must never reach a log line or a metric label.
// This scans every non-test roomkit source that handles it: a log or
// metric call naming a token variable fails the tests. Games run the same
// scan over their own sources.
func TestTokenNeverLogged(t *testing.T) {
	sink := regexp.MustCompile(`slog\.|\.(Info|Warn|Error|Debug|Inc|Observe)\(`)
	tok := regexp.MustCompile(`(?i)\btok\b|NewToken|fresh|pilot\.Header|X-Pilot-Token`)
	var files []string
	for _, dir := range []string{".", "../room", "../lobby", "../metrics", "../pilot"} {
		m, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		if len(m) == 0 {
			t.Errorf("directory %s matched no files", dir)
		}
		files = append(files, m...)
	}
	if len(files) < 5 {
		t.Fatalf("scanned only %d files", len(files))
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			if sink.MatchString(line) && tok.MatchString(line) {
				t.Errorf("%s:%d logs or labels a token: %s", f, i+1, strings.TrimSpace(line))
			}
		}
	}
}
