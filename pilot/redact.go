package pilot

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
)

// Redacted replaces a token in log output.
const Redacted = "[redacted]"

// Redact wraps h so no raw token reaches the log, whatever a call site
// passes: attributes named tok/token (any case, e.g. NewToken) are blanked,
// and any token-shaped word (TokenLen base64url characters that Valid
// accepts) in the message, a string, an error or a formatted struct, map or
// pointer is replaced. Defense in depth behind "never log the token".
func Redact(h slog.Handler) slog.Handler { return redactor{h} }

type redactor struct{ h slog.Handler }

func (r redactor) Enabled(ctx context.Context, l slog.Level) bool { return r.h.Enabled(ctx, l) }

func (r redactor) Handle(ctx context.Context, rec slog.Record) error {
	out := slog.NewRecord(rec.Time, rec.Level, scrubString(rec.Message), rec.PC)
	rec.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(scrub(a))
		return true
	})
	return r.h.Handle(ctx, out)
}

func (r redactor) WithAttrs(as []slog.Attr) slog.Handler {
	out := make([]slog.Attr, len(as))
	for i, a := range as {
		out[i] = scrub(a)
	}
	return redactor{r.h.WithAttrs(out)}
}

func (r redactor) WithGroup(name string) slog.Handler { return redactor{r.h.WithGroup(name)} }

func tokenKey(k string) bool {
	k = strings.ToLower(k)
	return k == "tok" || strings.Contains(k, "token")
}

func scrub(a slog.Attr) slog.Attr {
	if tokenKey(a.Key) {
		return slog.String(a.Key, Redacted)
	}
	v := a.Value.Resolve()
	switch v.Kind() {
	case slog.KindString:
		return slog.String(a.Key, scrubString(v.String()))
	case slog.KindGroup:
		g := v.Group()
		out := make([]slog.Attr, len(g))
		for i, x := range g {
			out[i] = scrub(x)
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(out...)}
	case slog.KindAny:
		// Structs, maps, pointers, errors: the text handler prints them with
		// %+v, the JSON handler marshals them (following pointers). A value
		// that holds a token in either form is replaced by its scrubbed text.
		x := v.Any()
		txt := fmt.Sprintf("%+v", x)
		js, _ := json.Marshal(x)
		if c := scrubString(txt); c != txt {
			return slog.String(a.Key, c)
		}
		if c := scrubString(string(js)); c != string(js) { // a token behind a pointer
			return slog.String(a.Key, c)
		}
	}
	return slog.Attr{Key: a.Key, Value: v}
}

// scrubString replaces every token-shaped word of s.
func scrubString(s string) string {
	var b strings.Builder
	start, changed := -1, false
	flush := func(end int) {
		if start < 0 {
			return
		}
		if w := s[start:end]; Valid(w) {
			b.WriteString(Redacted)
			changed = true
		} else {
			b.WriteString(w)
		}
		start = -1
	}
	for i := 0; i < len(s); i++ {
		if alphabet(s[i]) {
			if start < 0 {
				start = i
			}
			continue
		}
		flush(i)
		b.WriteByte(s[i])
	}
	flush(len(s))
	if !changed {
		return s
	}
	return b.String()
}

func alphabet(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_'
}
