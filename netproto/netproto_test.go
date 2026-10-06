package netproto

import (
	"encoding/json"
	"math"
	"testing"
)

func TestCoreMessagesWire(t *testing.T) {
	for _, c := range []struct {
		v    any
		want string
	}{
		{NewPong(5), `{"t":"pong","ts":5}`},
		{NewError(CodeFull, "oda dolu"), `{"t":"error","msg":"oda dolu","code":"full"}`},
		{NewError("", "x"), `{"t":"error","msg":"x"}`},
		{NewNotice("team_full", "Takım dolu"), `{"t":"notice","msg":"Takım dolu","code":"team_full"}`},
		{NewChat(3, 2), `{"t":"chat","from":3,"id":2}`},
	} {
		b, err := json.Marshal(c.v)
		if err != nil || string(b) != c.want {
			t.Errorf("%T: %s %v, want %s", c.v, b, err, c.want)
		}
	}
}

func TestCheckHeader(t *testing.T) {
	ok := []Header{{T: TPing, TS: 1}, {T: TChat, Chat: 1}, {T: TChat, Chat: 6}, {T: TIn, Seq: 1}}
	for _, h := range ok {
		if err := CheckHeader(h, 6); err != nil {
			t.Errorf("%+v: %v", h, err)
		}
	}
	if err := CheckHeader(Header{T: TChat, Chat: 7}, 6); err != ErrBadChat {
		t.Errorf("chat 7: %v", err)
	}
	if err := CheckHeader(Header{T: TChat}, 6); err != ErrBadChat {
		t.Errorf("chat 0: %v", err)
	}
	if err := CheckHeader(Header{T: TPing, TS: math.Inf(1)}, 6); err != ErrNotFinite {
		t.Errorf("inf ts: %v", err)
	}
}

func TestCleanName(t *testing.T) {
	for in, want := range map[string]string{
		"  Ace  ":                     "Ace",
		"":                            "Pilot",
		"\u202e\u200b\t":              "Pilot",
		"\u200b\u200b":                "Pilot",
		"a\u202eb\u2066c\u200fd\x00e": "abcde",
		"abcdefghijklmnopqrstuvwxyz":  "abcdefghijklmnop",
		"\u00e7\u011f\u00fc\u015f\u0130\u00f6\u00e7\u011f\u00fc\u015f\u0130\u00f6\u00e7\u011f\u00fc\u015f\u0130\u00f6XYZ": "\u00e7\u011f\u00fc\u015f\u0130\u00f6\u00e7\u011f\u00fc\u015f\u0130\u00f6\u00e7\u011f\u00fc\u015f",
		"\xff\xfeok":                             "ok",
		"\u3164\u115f\u1160\uffa0\u2800":         "Pilot", // blank-looking fillers
		"a\u3164b":                               "ab",
		"a\u0301\u0301\u0301\u0301\u0301b\u0300": "a\u0301\u0301b\u0300", // at most 2 marks per letter
		"\u00e9\u0301\u0301\u0301":               "\u00e9\u0301\u0301",   // precomposed letter plus marks: 2 marks kept
	} {
		if got := CleanName(in); got != want {
			t.Errorf("CleanName(%q) = %q, want %q", in, got, want)
		}
	}
}
