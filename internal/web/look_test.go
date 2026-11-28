package web

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// This file guards the markup and script of the Clear look (issue #217) that
// is not about icons or the palette: the bar label split of Memory & Swap
// and Filesystems (AC6), the 44 px touch targets (AC5) and the absence of
// inline styles, inline scripts and foreign origins (AC11). It reads the embedded assets as text.

// unconditionalLevelClass matches a percentage element whose class is
// always extended with the level class, ok included.
var unconditionalLevelClass = regexp.MustCompile(`['"]bar-pct ['"]\s*\+\s*cls`)

func TestAppJS_BarLabelSplit_AC6(t *testing.T) {
	js := readAsset(t, "app.js")
	html := readAsset(t, "index.html")

	t.Run("AC6 Memory & Swap labels hold a percentage and a byte element", func(t *testing.T) {
		checkBarLabelMarkup(t, html)
	})

	t.Run("AC6 renderBar fills percentage and bytes separately by textContent", func(t *testing.T) {
		checkRenderBar(t, js)
	})

	t.Run("AC6 barRow builds percentage and bytes as two elements by textContent", func(t *testing.T) {
		checkBarRow(t, js)
	})

	t.Run("AC6 callers pass the bytes element id", func(t *testing.T) {
		requireContains(t, "app.js", js,
			"renderBar('mem-bar', 'mem-pct', 'mem-bytes',",
			"renderBar('swap-bar', 'swap-pct', 'swap-bytes',",
		)
	})
}

// checkBarLabelMarkup checks that the Memory and Swap labels hold a percentage and a byte element.
func checkBarLabelMarkup(t *testing.T, html string) {
	t.Helper()
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
}

// checkRenderBar checks that renderBar fills the percentage and the bytes separately.
func checkRenderBar(t *testing.T, js string) {
	t.Helper()
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
}

// checkBarRow checks that barRow builds the percentage and the bytes as two elements.
func checkBarRow(t *testing.T, js string) {
	t.Helper()
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
}

// pxValue parses a whole-pixel length such as "44px"; ok is false for any
// other unit or spelling.
func pxValue(v string) (int, bool) {
	num, found := strings.CutSuffix(strings.TrimSpace(v), "px")
	if !found {
		return 0, false
	}
	n, err := strconv.Atoi(num)
	return n, err == nil
}

// The selectors of AC5: the first group needs a height of at least 44 px
// (min-height or height), the second one is icon-only and needs 44 px in both
// directions.
var (
	touchHeightOnly = []string{".card h2", ".card-header-btn", ".updates-button", ".range-button", ".layout-reset", ".apikey-submit"}
	touchSquare     = []string{".theme-toggle", ".layout-toggle", ".modal-close", ".layout-move"}
)

// atLeast44 reports whether decls give the dimension (its min- form or the
// plain property) at least 44 px.
func atLeast44(decls map[string]string, dim string) bool {
	for _, prop := range []string{"min-" + dim, dim} {
		if n, ok := pxValue(decls[prop]); ok && n >= 44 {
			return true
		}
	}
	return false
}

// touchTargetFailures returns one message per selector of AC5 that css does
// not size to at least 44 px.
func touchTargetFailures(css string) []string {
	rules := parseCSSRules(css)
	var out []string
	for _, sel := range touchHeightOnly {
		if !atLeast44(declsFor(rules, sel), "height") {
			out = append(out, sel+": height below 44px or missing")
		}
	}
	for _, sel := range touchSquare {
		d := declsFor(rules, sel)
		if !atLeast44(d, "height") || !atLeast44(d, "width") {
			out = append(out, sel+": width or height below 44px or missing")
		}
	}
	return out
}

func conformingTouchCSS() string {
	var b strings.Builder
	for _, sel := range touchHeightOnly {
		fmt.Fprintf(&b, "%s { min-height: 44px; }\n", sel)
	}
	for _, sel := range touchSquare {
		fmt.Fprintf(&b, "%s { width: 44px; height: 44px; }\n", sel)
	}
	return b.String()
}

func TestStyleCSS_TouchTargets_AC5(t *testing.T) {
	t.Run("AC5 every control is at least 44 px", func(t *testing.T) {
		for _, f := range touchTargetFailures(readAsset(t, "style.css")) {
			t.Error(f)
		}
	})

	t.Run("AC5 the scanner flags a small, missing or half-sized target", func(t *testing.T) {
		good := conformingTouchCSS()
		if got := touchTargetFailures(good); len(got) != 0 {
			t.Fatalf("scanner on a conforming sheet: got %v, want none", got)
		}
		cases := map[string]string{
			"43px high":      strings.Replace(good, ".range-button { min-height: 44px; }", ".range-button { min-height: 43px; }", 1),
			"unit in rem":    strings.Replace(good, ".updates-button { min-height: 44px; }", ".updates-button { min-height: 2.75rem; }", 1),
			"width only":     strings.Replace(good, ".modal-close { width: 44px; height: 44px; }", ".modal-close { width: 44px; }", 1),
			"rule missing":   strings.Replace(good, ".layout-reset { min-height: 44px; }", "", 1),
			"narrow icon":    strings.Replace(good, ".theme-toggle { width: 44px; height: 44px; }", ".theme-toggle { width: 36px; height: 44px; }", 1),
			"override later": good + ".card-header-btn { min-height: 20px; }\n",
		}
		for name, css := range cases {
			if css == good {
				t.Fatalf("%s: mutation did not change the sheet", name)
			}
			if len(touchTargetFailures(css)) == 0 {
				t.Errorf("%s: scanner reported nothing, want a failure", name)
			}
		}
	})
}

var (
	inlineStyleAttrRE = regexp.MustCompile(`(?i)<[a-z][^>]*\sstyle\s*=`)
	styleElementRE    = regexp.MustCompile(`(?i)<style[\s>]`)
	scriptElementRE   = regexp.MustCompile(`(?is)<script([^>]*)>(.*?)</script\s*>`)
	scriptSrcRE       = regexp.MustCompile(`(?i)\ssrc\s*=`)
)

// inlineFindings reports a style attribute, a style element and a script
// element that has no src or has a body.
func inlineFindings(html string) []string {
	var out []string
	if inlineStyleAttrRE.MatchString(html) {
		out = append(out, "style attribute")
	}
	if styleElementRE.MatchString(html) {
		out = append(out, "style element")
	}
	for _, m := range scriptElementRE.FindAllStringSubmatch(html, -1) {
		if !scriptSrcRE.MatchString(m[1]) || strings.TrimSpace(m[2]) != "" {
			out = append(out, "inline script")
		}
	}
	return out
}

func TestIndexHTML_NoInlineStyle_AC11(t *testing.T) {
	t.Run("AC11 index.html has no inline style or script", func(t *testing.T) {
		if got := inlineFindings(readAsset(t, "index.html")); len(got) != 0 {
			t.Errorf("index.html: found %v, want none", got)
		}
	})

	t.Run("AC11 the scanner flags each kind of inline code", func(t *testing.T) {
		if got := inlineFindings(`<script src="a.js"></script><div class="x"><svg viewBox="0 0 1 1"></svg></div>`); len(got) != 0 {
			t.Fatalf("scanner on clean markup: got %v, want none", got)
		}
		for name, html := range map[string]string{
			"style attribute":       `<div style="width:1px"></div>`,
			"style attr on svg":     "<svg\n  STYLE = 'fill:red'></svg>",
			"style element":         `<style>.a{}</style>`,
			"script body":           `<script>alert(1)</script>`,
			"script without src":    `<script></script>`,
			"script src plus body":  `<script src="a.js">x()</script>`,
			"upper case script tag": `<SCRIPT>x()</SCRIPT>`,
		} {
			if len(inlineFindings(html)) == 0 {
				t.Errorf("%s: scanner reported nothing, want a finding", name)
			}
		}
	})
}

const svgNamespace = "http://www.w3.org/2000/svg"

var (
	absoluteURLRE = regexp.MustCompile(`(?i)https?://[^\s'"<>)]*`)
	protoRelRE    = regexp.MustCompile(`["'(=]\s*//[A-Za-z0-9]`)
	cssImportRE   = regexp.MustCompile(`(?i)@import\b`)
)

// originFindings lists every reference to another origin: an absolute URL
// other than the SVG namespace, a protocol-relative URL or a CSS @import.
func originFindings(text string) []string {
	var out []string
	for _, u := range absoluteURLRE.FindAllString(text, -1) {
		if u != svgNamespace {
			out = append(out, u)
		}
	}
	out = append(out, protoRelRE.FindAllString(text, -1)...)
	if cssImportRE.MatchString(text) {
		out = append(out, "@import")
	}
	return out
}

func TestAssets_NoOtherOrigin_AC11(t *testing.T) {
	t.Run("AC11 no asset references another origin", func(t *testing.T) {
		for _, name := range []string{"index.html", "style.css", "app.js", "chart.js", "gauge.js", "theme-init.js"} {
			if got := originFindings(readAsset(t, name)); len(got) != 0 {
				t.Errorf("%s: found %v, want none", name, got)
			}
		}
	})

	t.Run("AC11 the scanner allows the SVG namespace only", func(t *testing.T) {
		if got := originFindings(`<svg xmlns='http://www.w3.org/2000/svg'> // a comment`); len(got) != 0 {
			t.Fatalf("scanner on the SVG namespace: got %v, want none", got)
		}
		for name, text := range map[string]string{
			"https url":           `<img src="https://example.com/a.png">`,
			"http url":            `fetch('http://example.com/x')`,
			"other xml namespace": `xmlns="http://www.w3.org/1999/xlink"`,
			"protocol relative":   `<script src="//cdn.example.com/a.js">`,
			"css url":             `background: url(//cdn.example.com/a.png)`,
			"css import":          `@import "a.css";`,
			"upper case scheme":   `HTTPS://example.com`,
		} {
			if len(originFindings(text)) == 0 {
				t.Errorf("%s: scanner reported nothing, want a finding", name)
			}
		}
	})
}

// headingBtnInheriting lists the properties a <button> does not inherit by
// default, so .card-header-btn has to set them explicitly to look like the
// plain .card h2 headings (issue #217, heading 12 px, weight 600, uppercase,
// letter-spacing 0.06em, --muted).
var headingBtnInheriting = []string{"text-transform", "letter-spacing"}

// headingBtnMatches reports whether the button's value for prop is
// "inherit" or the same value the heading has.
func headingBtnMatches(btn, h2 map[string]string, prop string) bool {
	v, ok := btn[prop]
	if !ok {
		return false
	}
	v = normCSS(v)
	return v == "inherit" || v == normCSS(h2[prop])
}

// headingBtnFindings returns the ways .card-header-btn departs from the
// typography of .card h2 in css.
func headingBtnFindings(css string) []string {
	rules := parseCSSRules(css)
	h2 := declsFor(rules, ".card h2")
	btn := declsFor(rules, ".card-header-btn")
	var out []string
	for _, prop := range headingBtnInheriting {
		if !headingBtnMatches(btn, h2, prop) {
			out = append(out, fmt.Sprintf(".card-header-btn %s = %q, want inherit or %q (a <button> does not inherit it)", prop, btn[prop], h2[prop]))
		}
	}
	if c := normCSS(btn["color"]); c != "inherit" && c != "var(--muted)" {
		out = append(out, fmt.Sprintf(".card-header-btn color = %q, want inherit or var(--muted)", btn["color"]))
	}
	if normCSS(btn["font"]) != "inherit" {
		out = append(out, fmt.Sprintf(".card-header-btn font = %q, want inherit", btn["font"]))
	}
	for _, prop := range []string{"font-size", "font-weight"} {
		if v, ok := btn[prop]; ok && normCSS(v) != "inherit" && normCSS(v) != normCSS(h2[prop]) {
			out = append(out, fmt.Sprintf(".card-header-btn %s = %q, want the heading's %q", prop, v, h2[prop]))
		}
	}
	return out
}

func TestStyleCSS_HeadingButtonTypography_AC5(t *testing.T) {
	t.Run("AC5 clickable card heading buttons look like the plain headings", func(t *testing.T) {
		for _, f := range headingBtnFindings(readAsset(t, "style.css")) {
			t.Error(f)
		}
	})

	t.Run("AC5 the check accepts inherit and rejects a missing reset", func(t *testing.T) {
		const h2 = ".card h2 { font-size: 12px; text-transform: uppercase; letter-spacing: 0.06em; color: var(--muted); font-weight: 600; }\n"
		good := h2 + ".card-header-btn { font: inherit; color: inherit; text-transform: inherit; letter-spacing: inherit; }\n"
		bad := h2 + ".card-header-btn { font: inherit; color: inherit; }\n"
		if got := headingBtnFindings(good); len(got) != 0 {
			t.Errorf("conforming CSS: got findings %v, want none", got)
		}
		if got := headingBtnFindings(bad); len(got) != 2 {
			t.Errorf("CSS without text-transform and letter-spacing: got %d findings %v, want 2", len(got), got)
		}
	})
}

var headerToggleSelectors = []string{".theme-toggle", ".layout-toggle"}

var nonNegativeLengthRE = regexp.MustCompile(`^\+?(?:\d+|\d*\.\d+)(?:px|rem|em)?$`)

// headerToggleFocusFindings checks the keyboard focus ring and the hover
// state of one header button selector.
func headerToggleFocusFindings(rules []cssRule, sel string) []string {
	var out []string
	focus := declsFor(rules, sel+":focus-visible")
	if len(focus) == 0 {
		return []string{sel + ":focus-visible has no rule"}
	}
	if normCSS(focus["outline"]) != "2pxsolidvar(--accent)" {
		out = append(out, fmt.Sprintf("%s:focus-visible outline = %q, want 2px solid var(--accent)", sel, focus["outline"]))
	}
	if !nonNegativeLengthRE.MatchString(strings.TrimSpace(focus["outline-offset"])) {
		out = append(out, fmt.Sprintf("%s:focus-visible outline-offset = %q, want a non-negative length", sel, focus["outline-offset"]))
	}
	return append(out, headerToggleHoverFindings(rules, sel)...)
}

// headerToggleHoverFindings requires the soft-halo hover: the border keeps
// its resting colour, the box-shadow adds a 3px accent halo to the resting
// shadow, and no outline competes with the focus ring.
func headerToggleHoverFindings(rules []cssRule, sel string) []string {
	var out []string
	hover := declsFor(rules, sel+":hover")
	if _, ok := hover["border-color"]; ok {
		out = append(out, sel+":hover sets border-color, want the resting border")
	}
	if _, ok := hover["outline"]; ok {
		out = append(out, sel+":hover sets outline, want a box-shadow halo")
	}
	shadow := normCSS(hover["box-shadow"])
	for _, part := range []string{"var(--shadow)", "0003px", "color-mix(insrgb,var(--accent)", "transparent"} {
		if !strings.Contains(shadow, part) {
			out = append(out, fmt.Sprintf("%s:hover box-shadow = %q, want it to contain %q", sel, hover["box-shadow"], part))
		}
	}
	return out
}

func TestStyleCSS_HeaderButtonFocusRing_AC5(t *testing.T) {
	t.Run("AC5 header buttons have an explicit accent focus ring and a border-only hover", func(t *testing.T) {
		rules := parseCSSRules(readAsset(t, "style.css"))
		for _, sel := range headerToggleSelectors {
			for _, f := range headerToggleFocusFindings(rules, sel) {
				t.Error(f)
			}
		}
	})

	t.Run("AC5 the check accepts a conforming rule and rejects a missing ring", func(t *testing.T) {
		const halo = ".theme-toggle:hover { box-shadow: var(--shadow), 0 0 0 3px color-mix(in srgb, var(--accent) 18%, transparent); }\n"
		const ring = ".theme-toggle:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }\n"
		badRing := ".theme-toggle:focus-visible { outline: 2px solid #fff; outline-offset: -2px; }\n"
		oldHover := ".theme-toggle:hover { border-color: var(--accent); }\n"
		if f := headerToggleFocusFindings(parseCSSRules(halo+ring), ".theme-toggle"); len(f) != 0 {
			t.Errorf("conforming CSS: got findings %v, want none", f)
		}
		if f := headerToggleFocusFindings(parseCSSRules(halo+badRing), ".theme-toggle"); len(f) != 2 {
			t.Errorf("literal colour and negative offset: got %d findings %v, want 2", len(f), f)
		}
		if f := headerToggleFocusFindings(parseCSSRules(halo), ".theme-toggle"); len(f) != 1 {
			t.Errorf("no focus rule: got %d findings %v, want 1", len(f), f)
		}
		if f := headerToggleHoverFindings(parseCSSRules(oldHover), ".theme-toggle"); len(f) != 5 {
			t.Errorf("border-colour hover without halo: got %d findings %v, want 5", len(f), f)
		}
	})
}
