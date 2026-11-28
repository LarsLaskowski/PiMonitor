package web

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// This file guards the icons of the Clear look (issue #217): no emoji in the
// dashboard markup and script (AC1), inline SVG icons (AC1), the chevron of
// the clickable cards (AC9), the mode icon swapped by CSS (AC10) and the
// accessible names of the network rates (AC13). It reads the embedded assets
// as text, like the other tests of this package.

// emojiRanges is the Unicode Extended_Pictographic property (Unicode 17.0),
// generated with
//
//	node -e 'const re=/\p{Extended_Pictographic}/u;let s=-1;for(let c=0;c<=0x10ffff;c++){const m=re.test(String.fromCodePoint(c));if(m&&s<0)s=c;if(!m&&s>=0){console.log("{0x"+s.toString(16)+", 0x"+(c-1).toString(16)+"},");s=-1}}'
//
// The arrows ↑ → ↓, the middle dot, the degree sign and the multiplication
// sign are not in it; the gear, the sun, the warning sign and the lightning
// bolt are.
var emojiRanges = [][2]rune{
	{0xa9, 0xa9}, {0xae, 0xae}, {0x203c, 0x203c}, {0x2049, 0x2049}, {0x2122, 0x2122}, {0x2139, 0x2139},
	{0x2194, 0x2199}, {0x21a9, 0x21aa}, {0x231a, 0x231b}, {0x2328, 0x2328}, {0x23cf, 0x23cf}, {0x23e9, 0x23f3},
	{0x23f8, 0x23fa}, {0x24c2, 0x24c2}, {0x25aa, 0x25ab}, {0x25b6, 0x25b6}, {0x25c0, 0x25c0}, {0x25fb, 0x25fe},
	{0x2600, 0x2604}, {0x260e, 0x260e}, {0x2611, 0x2611}, {0x2614, 0x2615}, {0x2618, 0x2618}, {0x261d, 0x261d},
	{0x2620, 0x2620}, {0x2622, 0x2623}, {0x2626, 0x2626}, {0x262a, 0x262a}, {0x262e, 0x262f}, {0x2638, 0x263a},
	{0x2640, 0x2640}, {0x2642, 0x2642}, {0x2648, 0x2653}, {0x265f, 0x2660}, {0x2663, 0x2663}, {0x2665, 0x2666},
	{0x2668, 0x2668}, {0x267b, 0x267b}, {0x267e, 0x267f}, {0x2692, 0x2697}, {0x2699, 0x2699}, {0x269b, 0x269c},
	{0x26a0, 0x26a1}, {0x26a7, 0x26a7}, {0x26aa, 0x26ab}, {0x26b0, 0x26b1}, {0x26bd, 0x26be}, {0x26c4, 0x26c5},
	{0x26c8, 0x26c8}, {0x26ce, 0x26cf}, {0x26d1, 0x26d1}, {0x26d3, 0x26d4}, {0x26e9, 0x26ea}, {0x26f0, 0x26f5},
	{0x26f7, 0x26fa}, {0x26fd, 0x26fd}, {0x2702, 0x2702}, {0x2705, 0x2705}, {0x2708, 0x270d}, {0x270f, 0x270f},
	{0x2712, 0x2712}, {0x2714, 0x2714}, {0x2716, 0x2716}, {0x271d, 0x271d}, {0x2721, 0x2721}, {0x2728, 0x2728},
	{0x2733, 0x2734}, {0x2744, 0x2744}, {0x2747, 0x2747}, {0x274c, 0x274c}, {0x274e, 0x274e}, {0x2753, 0x2755},
	{0x2757, 0x2757}, {0x2763, 0x2764}, {0x2795, 0x2797}, {0x27a1, 0x27a1}, {0x27b0, 0x27b0}, {0x27bf, 0x27bf},
	{0x2934, 0x2935}, {0x2b05, 0x2b07}, {0x2b1b, 0x2b1c}, {0x2b50, 0x2b50}, {0x2b55, 0x2b55}, {0x3030, 0x3030},
	{0x303d, 0x303d}, {0x3297, 0x3297}, {0x3299, 0x3299}, {0x1f004, 0x1f004}, {0x1f02c, 0x1f02f},
	{0x1f094, 0x1f09f}, {0x1f0af, 0x1f0b0}, {0x1f0c0, 0x1f0c0}, {0x1f0cf, 0x1f0d0}, {0x1f0f6, 0x1f0ff},
	{0x1f170, 0x1f171}, {0x1f17e, 0x1f17f}, {0x1f18e, 0x1f18e}, {0x1f191, 0x1f19a}, {0x1f1ae, 0x1f1e5},
	{0x1f201, 0x1f20f}, {0x1f21a, 0x1f21a}, {0x1f22f, 0x1f22f}, {0x1f232, 0x1f23a}, {0x1f23c, 0x1f23f},
	{0x1f249, 0x1f25f}, {0x1f266, 0x1f321}, {0x1f324, 0x1f393}, {0x1f396, 0x1f397}, {0x1f399, 0x1f39b},
	{0x1f39e, 0x1f3f0}, {0x1f3f3, 0x1f3f5}, {0x1f3f7, 0x1f3fa}, {0x1f400, 0x1f4fd}, {0x1f4ff, 0x1f53d},
	{0x1f549, 0x1f54e}, {0x1f550, 0x1f567}, {0x1f56f, 0x1f570}, {0x1f573, 0x1f57a}, {0x1f587, 0x1f587},
	{0x1f58a, 0x1f58d}, {0x1f590, 0x1f590}, {0x1f595, 0x1f596}, {0x1f5a4, 0x1f5a5}, {0x1f5a8, 0x1f5a8},
	{0x1f5b1, 0x1f5b2}, {0x1f5bc, 0x1f5bc}, {0x1f5c2, 0x1f5c4}, {0x1f5d1, 0x1f5d3}, {0x1f5dc, 0x1f5de},
	{0x1f5e1, 0x1f5e1}, {0x1f5e3, 0x1f5e3}, {0x1f5e8, 0x1f5e8}, {0x1f5ef, 0x1f5ef}, {0x1f5f3, 0x1f5f3},
	{0x1f5fa, 0x1f64f}, {0x1f680, 0x1f6c5}, {0x1f6cb, 0x1f6d2}, {0x1f6d5, 0x1f6e5}, {0x1f6e9, 0x1f6e9},
	{0x1f6eb, 0x1f6f0}, {0x1f6f3, 0x1f6ff}, {0x1f7da, 0x1f7ff}, {0x1f80c, 0x1f80f}, {0x1f848, 0x1f84f},
	{0x1f85a, 0x1f85f}, {0x1f888, 0x1f88f}, {0x1f8ae, 0x1f8af}, {0x1f8bc, 0x1f8bf}, {0x1f8c2, 0x1f8cf},
	{0x1f8d9, 0x1f8ff}, {0x1f90c, 0x1f93a}, {0x1f93c, 0x1f945}, {0x1f947, 0x1f9ff}, {0x1fa58, 0x1fa5f},
	{0x1fa6e, 0x1faff}, {0x1fc00, 0x1fffd},
}

const variationSelector16 rune = 0xfe0f

var (
	htmlNumRefRE = regexp.MustCompile(`&#(?:([0-9]+)|[xX]([0-9a-fA-F]+));`)
	jsEscapeRE   = regexp.MustCompile(`\\u\{([0-9a-fA-F]+)\}|\\u([dD][89abAB][0-9a-fA-F]{2})\\u([dD][c-fC-F][0-9a-fA-F]{2})|\\u([0-9a-fA-F]{4})`)
	svgTagRE     = regexp.MustCompile(`<svg\b[^>]*>`)
)

// isEmojiRune reports whether r is an Extended_Pictographic code point or
// the emoji variation selector U+FE0F.
func isEmojiRune(r rune) bool {
	if r == variationSelector16 {
		return true
	}
	i := sort.Search(len(emojiRanges), func(i int) bool { return emojiRanges[i][1] >= r })
	return i < len(emojiRanges) && emojiRanges[i][0] <= r
}

func parseCodePoint(digits string, base int) rune {
	n, err := strconv.ParseInt(digits, base, 32)
	if err != nil {
		return -1
	}
	return rune(n)
}

// surrogatePair combines a UTF-16 high and low surrogate (hex text).
func surrogatePair(high, low string) rune {
	h, l := parseCodePoint(high, 16), parseCodePoint(low, 16)
	return 0x10000 + (h-0xd800)<<10 + (l - 0xdc00)
}

// findEmoji lists every emoji of src: literal, as an HTML character
// reference, or as a JavaScript escape (\uXXXX, a surrogate pair, \u{…}).
func findEmoji(src string) []string {
	var found []string
	for _, r := range src {
		if isEmojiRune(r) {
			found = append(found, fmt.Sprintf("literal U+%04X", r))
		}
	}
	for _, m := range htmlNumRefRE.FindAllStringSubmatch(src, -1) {
		r := parseCodePoint(m[1], 10)
		if m[1] == "" {
			r = parseCodePoint(m[2], 16)
		}
		if isEmojiRune(r) {
			found = append(found, fmt.Sprintf("character reference %s = U+%04X", m[0], r))
		}
	}
	for _, m := range jsEscapeRE.FindAllStringSubmatch(src, -1) {
		var r rune
		switch {
		case m[1] != "":
			r = parseCodePoint(m[1], 16)
		case m[2] != "":
			r = surrogatePair(m[2], m[3])
		default:
			r = parseCodePoint(m[4], 16)
		}
		if isEmojiRune(r) {
			found = append(found, fmt.Sprintf("JavaScript escape %s = U+%04X", m[0], r))
		}
	}
	return found
}

func TestAssets_NoEmoji_AC1(t *testing.T) {
	t.Run("AC1 scanner catches emoji in every spelling and spares the arrows", func(t *testing.T) {
		cases := []struct {
			name string
			src  string
			want bool
		}{
			{"character references of the layout button", `&#9881;&#65039;`, true},
			{"literal sun", "☀", true},
			{"literal gear", "⚙", true},
			{"literal computer", "\U0001F5A5", true},
			{"variation selector alone", "️", true},
			{"hex character reference", `&#x2699;`, true},
			{"JavaScript BMP escape", `'⚡'`, true},
			{"JavaScript surrogate pair", `'🌙'`, true},
			{"JavaScript braced escape", `'\u{1F319}'`, true},
			{"JavaScript variation selector escape", `'️'`, true},
			{"rightwards arrow", "→", false},
			{"up and down arrows", "↑ ↓", false},
			{"JavaScript arrow and middle dot escapes", `'→ ·'`, false},
			{"arrow character reference", `&#8593;`, false},
			{"named references", `&times; &middot; &ndash;`, false},
			{"degree sign and middle dot", "° ·", false},
		}
		for _, c := range cases {
			if got := len(findEmoji(c.src)) > 0; got != c.want {
				t.Errorf("%s: findEmoji(%q) found emoji = %v, want %v", c.name, c.src, got, c.want)
			}
		}
	})

	for _, name := range []string{"index.html", "app.js"} {
		t.Run("AC1 "+name+" holds no emoji", func(t *testing.T) {
			for _, f := range findEmoji(readAsset(t, name)) {
				t.Errorf("%s: emoji found: %s", name, f)
			}
		})
	}
}

// requireHiddenSVGs fails unless region holds at least min inline SVG icons
// and each one has viewBox="0 0 24 24" and aria-hidden="true".
func requireHiddenSVGs(t *testing.T, where, region string, min int) {
	t.Helper()
	tags := svgTagRE.FindAllString(region, -1)
	if len(tags) < min {
		t.Errorf("index.html: %s holds %d inline <svg>, want at least %d", where, len(tags), min)
	}
	for _, tag := range tags {
		if !strings.Contains(tag, `viewBox="0 0 24 24"`) {
			t.Errorf(`index.html: %s: %s lacks viewBox="0 0 24 24"`, where, tag)
		}
		if !strings.Contains(tag, `aria-hidden="true"`) {
			t.Errorf(`index.html: %s: %s lacks aria-hidden="true"`, where, tag)
		}
	}
}

// buttonContent returns the inner text of the <button> with the given id.
func buttonContent(html, id string) string {
	re := regexp.MustCompile(`(?s)<button\b[^>]*\bid="` + regexp.QuoteMeta(id) + `"[^>]*>(.*?)</button>`)
	if m := re.FindStringSubmatch(html); m != nil {
		return m[1]
	}
	return ""
}

func TestIndexHTML_IconsAreInlineSVG_AC1(t *testing.T) {
	html := readAsset(t, "index.html")

	t.Run("AC1 every card heading shows an inline SVG icon", func(t *testing.T) {
		headings := regexp.MustCompile(`(?s)id="(card-\w+)">\s*<h2>(.*?)</h2>`).FindAllStringSubmatch(html, -1)
		if len(headings) != 10 {
			t.Fatalf("index.html: %d card headings, want 10", len(headings))
		}
		for _, h := range headings {
			requireHiddenSVGs(t, h[1]+" heading", h[2], 1)
		}
	})

	t.Run("AC1 header logo tile, mode and layout buttons show an inline SVG icon", func(t *testing.T) {
		tile := regexp.MustCompile(`(?s)<span class="logo-tile"[^>]*>(.*?)</span>`).FindStringSubmatch(html)
		if tile == nil {
			t.Error(`index.html: expected a <span class="logo-tile"> in the header`)
		} else {
			requireHiddenSVGs(t, "logo tile", tile[1], 1)
		}
		requireHiddenSVGs(t, "#layout-toggle", buttonContent(html, "layout-toggle"), 1)
		requireHiddenSVGs(t, "#theme-toggle", buttonContent(html, "theme-toggle"), 2)
	})

	t.Run("AC1 alert banner shows an inline SVG icon", func(t *testing.T) {
		start := strings.Index(html, `id="alert-banner"`)
		end := strings.Index(html, `id="alert-banner-text"`)
		if start < 0 || end < start {
			t.Fatal("index.html: expected #alert-banner followed by #alert-banner-text")
		}
		requireHiddenSVGs(t, "#alert-banner", html[start:end], 1)
	})

	t.Run("AC1 favicon is an SVG drawing in a data URI", func(t *testing.T) {
		m := regexp.MustCompile(`<link rel="icon" href="([^"]*)">`).FindStringSubmatch(html)
		if m == nil {
			t.Fatal(`index.html: expected a <link rel="icon" href="…">`)
		}
		href := m[1]
		if !strings.HasPrefix(href, "data:image/svg+xml") {
			t.Errorf("favicon href = %.40q…, want a data:image/svg+xml URI", href)
		}
		if strings.Contains(href, "<text") || strings.Contains(href, "%3Ctext") {
			t.Error("favicon: expected a drawing, not a <text> glyph")
		}
	})
}

func TestIndexHTML_ClickableCardChevron_AC9(t *testing.T) {
	html := readAsset(t, "index.html")
	labels := map[string]string{
		"cpu":         "Open CPU usage detail",
		"load":        "Open load average detail",
		"temperature": "Open temperature detail",
		"memory":      "Open Memory &amp; Swap detail",
	}
	for _, metric := range []string{"cpu", "load", "temperature", "memory"} {
		t.Run("AC9 "+metric+" heading button ends with a chevron", func(t *testing.T) {
			re := regexp.MustCompile(`(?s)data-metric="` + metric + `".*?<button type="button" class="card-header-btn" aria-label="([^"]*)">(.*?)</button>`)
			m := re.FindStringSubmatch(html)
			if m == nil {
				t.Fatalf("index.html: no heading button in the %s card", metric)
			}
			if m[1] != labels[metric] {
				t.Errorf("%s heading button aria-label = %q, want %q", metric, m[1], labels[metric])
			}
			requireHiddenSVGs(t, metric+" heading button", m[2], 2)
			if !strings.HasSuffix(strings.TrimSpace(m[2]), "</svg>") {
				t.Errorf("%s heading button: expected an aria-hidden chevron <svg> as its last child, got %q", metric, m[2])
			}
		})
	}
}

// isShown reports whether a display value makes an element visible.
func isShown(display string) bool {
	return display != "" && display != "none"
}

func TestModeToggle_IconByCSS_AC10(t *testing.T) {
	html := readAsset(t, "index.html")
	css := readAsset(t, "style.css")
	js := readAsset(t, "app.js")

	t.Run("AC10 both mode icons sit in the toggle button", func(t *testing.T) {
		content := buttonContent(html, "theme-toggle")
		for _, class := range []string{"mode-icon-moon", "mode-icon-sun"} {
			if !strings.Contains(content, class) {
				t.Errorf("#theme-toggle: expected an element of class %s", class)
			}
		}
	})

	t.Run("AC10 CSS shows the moon in light mode and the sun in both dark selectors", func(t *testing.T) {
		rules := parseCSSRules(css)
		if got := declsFor(rules, ".mode-icon-sun")["display"]; got != "none" {
			t.Errorf(".mode-icon-sun { display } = %q, want none by default", got)
		}
		if got := declsFor(rules, ".mode-icon-moon")["display"]; got == "none" {
			t.Error(".mode-icon-moon { display } = none, want it shown by default")
		}
		for _, root := range []string{selDark, selMediaDark} {
			if got := declsFor(rules, root+" .mode-icon-moon")["display"]; got != "none" {
				t.Errorf("%s .mode-icon-moon { display } = %q, want none", root, got)
			}
			if got := declsFor(rules, root+" .mode-icon-sun")["display"]; !isShown(got) {
				t.Errorf("%s .mode-icon-sun { display } = %q, want it shown", root, got)
			}
		}
	})

	t.Run("AC10 updateThemeToggle keeps its labels and writes no text", func(t *testing.T) {
		body := jsFunctionBody(js, "function updateThemeToggle()")
		if strings.TrimSpace(body) == "" {
			t.Fatal("app.js: expected a function updateThemeToggle()")
		}
		requireContains(t, "updateThemeToggle", body,
			"btn.setAttribute('aria-label', dark ? 'Switch to light theme' : 'Switch to dark theme');",
			"btn.setAttribute('aria-pressed', String(dark));",
		)
		if strings.Contains(body, "textContent") {
			t.Error("updateThemeToggle: expected no textContent write, the icon is shown by CSS")
		}
	})

	t.Run("AC10 applyTheme still redraws charts and gauges", func(t *testing.T) {
		requireContains(t, "applyTheme", jsFunctionBody(js, "function applyTheme(theme)"),
			"updateThemeToggle();", "renderMetrics(latestSnapshot)", "renderHistory(latestHistory)")
	})
}

func TestAppJS_NetworkRateNames_AC13(t *testing.T) {
	html := readAsset(t, "index.html")
	js := readAsset(t, "app.js")

	t.Run("AC13 arrow templates exist in the page", func(t *testing.T) {
		for _, id := range []string{"tpl-icon-download", "tpl-icon-upload"} {
			m := regexp.MustCompile(`(?s)<template id="` + id + `">(.*?)</template>`).FindStringSubmatch(html)
			if m == nil {
				t.Errorf("index.html: expected <template id=%q>", id)
				continue
			}
			requireHiddenSVGs(t, id, m[1], 1)
		}
	})

	t.Run("AC13 networkRow clones the icons and names the rates for screen readers", func(t *testing.T) {
		body := jsFunctionBody(js, "function networkRow(n)")
		if strings.TrimSpace(body) == "" {
			t.Fatal("app.js: expected a function networkRow(n)")
		}
		requireContains(t, "networkRow", body, "tpl-icon-download", "tpl-icon-upload", "sr-only", "textContent")
		for _, word := range []string{"Download", "Upload"} {
			if !regexp.MustCompile(`['"]` + word + ` ['"]`).MatchString(body) {
				t.Errorf("networkRow: expected the text %q followed by a space as a string", word+" ")
			}
		}
		if !strings.Contains(body, "cloneNode(true)") && !strings.Contains(body, "importNode(") {
			t.Error("networkRow: expected the icon templates to be cloned (cloneNode(true) or importNode)")
		}
		if strings.Contains(body, "innerHTML") {
			t.Error("networkRow: expected no innerHTML")
		}
		if !strings.Contains(jsFunctionBody(js, "function applyNetworkVisibility(snap)"), "networkRow(") {
			t.Error("applyNetworkVisibility: expected rows built by networkRow(")
		}
	})
}
