package web

import (
	"regexp"
	"strings"
	"testing"
)

// TestAppJS_NoInnerHTMLInterpolation guards against stored XSS via the
// dashboard building DOM rows by concatenating server-provided strings
// (mountpoints, network interface names) into innerHTML (issue #19).
// The only permitted use of innerHTML is clearing a container with an
// empty string literal; everything else must go through
// document.createElement + textContent.
func TestAppJS_NoInnerHTMLInterpolation(t *testing.T) {
	data, err := assetsFS.ReadFile("assets/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}

	// Allowed: clearing assignments like `el.innerHTML = '';` (single or
	// double quotes, optional trailing semicolon).
	clearing := regexp.MustCompile(`innerHTML\s*=\s*(''|"")\s*;?\s*$`)

	for i, line := range strings.Split(string(data), "\n") {
		if !strings.Contains(line, "innerHTML") {
			continue
		}
		if clearing.MatchString(strings.TrimSpace(line)) {
			continue
		}
		t.Errorf("app.js line %d uses innerHTML with non-empty content: %s", i+1, strings.TrimSpace(line))
	}
}
