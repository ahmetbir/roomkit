package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
)

// bundle lists the fixed-name build outputs index.html references.
var bundle = []string{"app.js", "style.css", "hud.css", "mobile.css"}

// fingerprint rewrites index.html's references to the bundle into
// content-hashed URLs ("/app.js?v=<hash>"). Cloudflare's zone-wide browser
// cache TTL overrides our no-cache on fixed names, so a deploy would
// otherwise keep serving browsers the old bundle for hours.
func fingerprint(web fs.FS, page []byte) []byte {
	for _, name := range bundle {
		b, err := fs.ReadFile(web, name)
		if err != nil {
			continue
		}
		sum := sha256.Sum256(b)
		ref := []byte(`"/` + name + `"`)
		versioned := []byte(`"/` + name + `?v=` + hex.EncodeToString(sum[:6]) + `"`)
		page = bytes.ReplaceAll(page, ref, versioned)
	}
	return page
}
