// Package tsconst reads constants out of the TypeScript client core, so a
// Go test can pin a Go constant to its TS twin: a drift on either side
// fails the build's tests instead of the wire.
package tsconst

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
)

// clientCore is the TS client core, relative to the module root.
const clientCore = "ts"

// source returns the text of file, relative to ts; the module
// root is found by walking up from the working directory to go.mod.
func source(file string) (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			b, err := os.ReadFile(filepath.Join(dir, clientCore, file))
			return string(b), err
		}
		up := filepath.Dir(dir)
		if up == dir {
			return "", fmt.Errorf("tsconst: no go.mod above the working directory")
		}
		dir = up
	}
}

// Int is `[export] const name = <integer>` in file.
func Int(file, name string) (int, error) {
	src, err := source(file)
	if err != nil {
		return 0, err
	}
	m := regexp.MustCompile(`(?m)^(?:export )?const ` + regexp.QuoteMeta(name) + ` = (\d+);`).FindStringSubmatch(src)
	if m == nil {
		return 0, fmt.Errorf("tsconst: %s lacks an integer const %s", file, name)
	}
	return strconv.Atoi(m[1])
}

// Strings is the string literals of `export const name = [...] as const` in file.
func Strings(file, name string) ([]string, error) {
	src, err := source(file)
	if err != nil {
		return nil, err
	}
	m := regexp.MustCompile(`(?s)export const ` + regexp.QuoteMeta(name) + ` = \[(.*?)\] as const`).FindStringSubmatch(src)
	if m == nil {
		return nil, fmt.Errorf("tsconst: %s lacks %s", file, name)
	}
	var out []string
	for _, q := range regexp.MustCompile(`"([^"]*)"`).FindAllStringSubmatch(m[1], -1) {
		out = append(out, q[1])
	}
	return out, nil
}

// Fields is the field names of the one-line `export type name = { a: …; b?: … }`
// in file, in order (a trailing "?" dropped).
func Fields(file, name string) ([]string, error) {
	src, err := source(file)
	if err != nil {
		return nil, err
	}
	m := regexp.MustCompile(`(?m)^export type ` + regexp.QuoteMeta(name) + ` = \{ (.*) \};$`).FindStringSubmatch(src)
	if m == nil {
		return nil, fmt.Errorf("tsconst: %s lacks a one-line type %s", file, name)
	}
	var out []string
	for _, f := range regexp.MustCompile(`(\w+)\??:`).FindAllStringSubmatch(m[1], -1) {
		out = append(out, f[1])
	}
	return out, nil
}
