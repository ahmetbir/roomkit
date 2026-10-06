package pilot

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

type hello struct {
	Name string
	Tok  string `json:"tok"`
}

type who struct {
	Name     string
	NewToken string
	Inner    *hello
}

func TestRedactKeepsTokensOutOfLogs(t *testing.T) {
	tok := New()
	for _, format := range []string{"text", "json"} {
		var buf bytes.Buffer
		var h slog.Handler = slog.NewTextHandler(&buf, nil)
		if format == "json" {
			h = slog.NewJSONHandler(&buf, nil)
		}
		log := slog.New(Redact(h))
		log.Info("hello "+tok, "tok", tok, "token", "x")
		log.Info("join", "who", who{Name: "a", NewToken: tok, Inner: &hello{"a", tok}})
		log.Info("msg", "m", hello{Name: "a", Tok: tok}, "ptr", &hello{Tok: tok})
		log.Info("str", "line", "client said "+tok+" twice: "+tok)
		log.Error("err", "err", errors.New("bad header "+tok))
		log.Info("group", slog.Group("g", "x", tok, slog.Group("h", "y", map[string]string{"k": tok})))
		log.With("w", tok).WithGroup("grp").Info("with", "z", fmt.Sprint([]string{tok}))
		out := buf.String()
		if strings.Contains(out, tok) {
			t.Fatalf("%s: token leaked:\n%s", format, out)
		}
		if strings.Count(out, "\n") != 7 || !strings.Contains(out, "join") || !strings.Contains(out, Redacted) {
			t.Fatalf("%s: lines lost or not marked:\n%s", format, out)
		}
	}
}

// Ordinary values pass untouched: hashes, names, short codes.
func TestRedactLeavesOtherValues(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(Redact(slog.NewTextHandler(&buf, nil)))
	h := Hash(New())
	log.Info("room created", "code", "ABCD", "pilot", h, "n", 3, "name", "Maverick")
	out := buf.String()
	if strings.Contains(out, Redacted) || !strings.Contains(out, h) || !strings.Contains(out, "code=ABCD") || !strings.Contains(out, "n=3") {
		t.Fatalf("over-redacted:\n%s", out)
	}
}
