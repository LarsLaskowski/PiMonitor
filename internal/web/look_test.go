package web

import (
	"regexp"
	"strings"
	"testing"
)

// This file guards the markup and script of the Clear look (issue #217) that
// is not about icons or the palette: the bar label split of Memory & Swap
// and Filesystems (AC6). It reads the embedded assets as text.

// unconditionalLevelClass matches a percentage element whose class is
// always extended with the level class, ok included.
var unconditionalLevelClass = regexp.MustCompile(`['"]bar-pct ['"]\s*\+\s*cls`)

func TestAppJS_BarLabelSplit_AC6(t *testing.T) {
	js := readAsset(t, "app.js")
	html := readAsset(t, "index.html")

	t.Run("AC6 Memory & Swap labels hold a percentage and a byte element", func(t *testing.T) {
		for _, prefix := range []string{"mem", "swap"} {
			for _, want := range []string{
				`<span class="bar-pct" id="` + prefix + `-pct">`,
				`<span class="bar-bytes" id="` + prefix + `-bytes">`,
			} {
				if !strings.Contains(html, want) {
					t.Errorf("index.html: expected %s", want)
				}
			}
		}
	})

	t.Run("AC6 renderBar fills percentage and bytes separately by textContent", func(t *testing.T) {
		body := jsFunctionBody(js, "function renderBar(barId, pctId, bytesId, value, warn, crit, bytesText)")
		if strings.TrimSpace(body) == "" {
			t.Fatal("app.js: expected a function renderBar(barId, pctId, bytesId, value, warn, crit, bytesText)")
		}
		requireContains(t, "renderBar", body,
			"setText(pctId, value.toFixed(1) + ' %')",
			"'· ' + bytesText",
			"setText(bytesId,",
			"metric-ok",
		)
		if unconditionalLevelClass.MatchString(body) {
			t.Error("renderBar: expected the level class on the percentage only at warn and crit, not always")
		}
		if strings.Contains(body, "innerHTML") {
			t.Error("renderBar: expected no innerHTML")
		}
	})

	t.Run("AC6 barRow builds percentage and bytes as two elements by textContent", func(t *testing.T) {
		body := jsFunctionBody(js, "function barRow(name, pct, warn, crit, bytesText)")
		if strings.TrimSpace(body) == "" {
			t.Fatal("app.js: expected a function barRow(name, pct, warn, crit, bytesText)")
		}
		requireContains(t, "barRow", body,
			"'bar-pct'",
			"'bar-bytes'",
			"pct.toFixed(1) + '%'",
			"'· ' + bytesText",
			"metric-ok",
		)
		if strings.Contains(body, "'% · '") {
			t.Error("barRow: expected percentage and bytes in separate elements, not one text")
		}
		if unconditionalLevelClass.MatchString(body) {
			t.Error("barRow: expected the level class on the percentage only at warn and crit, not always")
		}
		if strings.Contains(body, "innerHTML") {
			t.Error("barRow: expected no innerHTML")
		}
		if got := strings.Count(body, "textContent"); got < 3 {
			t.Errorf("barRow: textContent written %d times, want at least 3 (name, percentage, bytes)", got)
		}
	})

	t.Run("AC6 callers pass the bytes element id", func(t *testing.T) {
		requireContains(t, "app.js", js,
			"renderBar('mem-bar', 'mem-pct', 'mem-bytes',",
			"renderBar('swap-bar', 'swap-pct', 'swap-bytes',",
		)
	})
}
