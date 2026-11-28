package web

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// This file guards the Clear palette of style.css (issue #217): the token
// blocks, the absence of colour literals outside them, the status-ink rules,
// the card chips and the WCAG contrast of the token pairs. It reads the
// embedded stylesheet as text, like the other tests of this package.

const (
	selLight      = ":root"
	selDark       = `:root[data-theme="dark"]`
	selMediaDark  = ":root:not([data-theme])"
	mediaDarkHead = `@media (prefers-color-scheme: dark) {`
)

var (
	cssCommentRE   = regexp.MustCompile(`(?s)/\*.*?\*/`)
	cssRuleRE      = regexp.MustCompile(`([^{}]*)\{([^{}]*)\}`)
	cssLeadZeroRE  = regexp.MustCompile(`(^|[^0-9])0\.`)
	cssTrailZeroRE = regexp.MustCompile(`(\.\d*[1-9])0+(\D|$)`)
	cssURLRE       = regexp.MustCompile(`url\([^)]*\)`)
	cssStringRE    = regexp.MustCompile(`"[^"]*"|'[^']*'`)
	cssHexRE       = regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b`)
	cssColourFnRE  = regexp.MustCompile(`(?i)\b(rgb|rgba|hsl|hsla|hwb|lab|lch|oklab|oklch)\(`)
	cssIdentRE     = regexp.MustCompile(`-{0,2}[a-zA-Z][a-zA-Z0-9-]*`)
	cssBaseTokenRE = regexp.MustCompile(`var\(\s*--(ok|warn|crit)\s*[,)]`)
	cssMediaDarkRE = regexp.MustCompile(`@media\s*\(\s*prefers-color-scheme:\s*dark\s*\)\s*\{\s*:root:not\(\[data-theme\]\)\s*\{`)
)

// cssNamedColours holds the CSS named colours that must not appear outside
// the palette blocks (transparent, currentColor and inherit are allowed).
var cssNamedColours = map[string]bool{}

func init() {
	names := `aliceblue antiquewhite aqua aquamarine azure beige bisque black blanchedalmond blue blueviolet brown
burlywood cadetblue chartreuse chocolate coral cornflowerblue cornsilk crimson cyan darkblue darkcyan darkgoldenrod
darkgray darkgreen darkgrey darkkhaki darkmagenta darkolivegreen darkorange darkorchid darkred darksalmon
darkseagreen darkslateblue darkslategray darkslategrey darkturquoise darkviolet deeppink deepskyblue dimgray dimgrey
dodgerblue firebrick floralwhite forestgreen fuchsia gainsboro ghostwhite gold goldenrod gray green greenyellow grey
honeydew hotpink indianred indigo ivory khaki lavender lavenderblush lawngreen lemonchiffon lightblue lightcoral
lightcyan lightgoldenrodyellow lightgray lightgreen lightgrey lightpink lightsalmon lightseagreen lightskyblue
lightslategray lightslategrey lightsteelblue lightyellow lime limegreen linen magenta maroon mediumaquamarine
mediumblue mediumorchid mediumpurple mediumseagreen mediumslateblue mediumspringgreen mediumturquoise
mediumvioletred midnightblue mintcream mistyrose moccasin navajowhite navy oldlace olive olivedrab orange orangered
orchid palegoldenrod palegreen paleturquoise palevioletred papayawhip peachpuff peru pink plum powderblue purple
rebeccapurple red rosybrown royalblue saddlebrown salmon sandybrown seagreen seashell sienna silver skyblue
slateblue slategray slategrey snow springgreen steelblue tan teal thistle tomato turquoise violet wheat white
whitesmoke yellow yellowgreen`
	for _, n := range strings.Fields(names) {
		cssNamedColours[n] = true
	}
}

type cssDecl struct{ prop, value string }

type cssRule struct {
	selectors []string
	decls     []cssDecl
}

// parseCSSRules returns every innermost rule of css (rules inside @media
// included) with its comma-separated selectors and its declarations in
// source order. Comments are removed first.
func parseCSSRules(css string) []cssRule {
	var rules []cssRule
	for _, m := range cssRuleRE.FindAllStringSubmatch(cssCommentRE.ReplaceAllString(css, ""), -1) {
		var r cssRule
		for _, s := range strings.Split(m[1], ",") {
			if s = strings.Join(strings.Fields(s), " "); s != "" {
				r.selectors = append(r.selectors, s)
			}
		}
		for _, d := range strings.Split(m[2], ";") {
			prop, value, ok := strings.Cut(d, ":")
			if ok {
				r.decls = append(r.decls, cssDecl{strings.TrimSpace(prop), strings.Join(strings.Fields(value), " ")})
			}
		}
		rules = append(rules, r)
	}
	return rules
}

func (r cssRule) has(selector string) bool {
	for _, s := range r.selectors {
		if s == selector {
			return true
		}
	}
	return false
}

func isPaletteRule(r cssRule) bool {
	return r.has(selLight) || r.has(selDark) || r.has(selMediaDark)
}

// declsFor merges the declarations of every rule whose selector list holds
// selector; a later declaration wins.
func declsFor(rules []cssRule, selector string) map[string]string {
	out := map[string]string{}
	for _, r := range rules {
		if r.has(selector) {
			for _, d := range r.decls {
				out[d.prop] = d.value
			}
		}
	}
	return out
}

// normCSS makes values comparable across spelling: case, whitespace, a
// leading zero before the decimal point and trailing decimal zeros.
func normCSS(v string) string {
	v = strings.ToLower(strings.Join(strings.Fields(v), ""))
	v = cssLeadZeroRE.ReplaceAllString(v, "${1}.")
	return cssTrailZeroRE.ReplaceAllString(v, "${1}${2}")
}

// paletteBlocks returns the custom properties of the light block, the
// explicit dark block and the prefers-color-scheme dark block.
func paletteBlocks(t *testing.T, css string) (light, dark, media map[string]string) {
	t.Helper()
	if !cssMediaDarkRE.MatchString(cssCommentRE.ReplaceAllString(css, "")) {
		t.Errorf("style.css: expected %s :root:not([data-theme]) { … } for the OS dark mode", mediaDarkHead)
	}
	rules := parseCSSRules(css)
	tokens := func(selector string) map[string]string {
		out := map[string]string{}
		found := 0
		for _, r := range rules {
			if !r.has(selector) {
				continue
			}
			found++
			for _, d := range r.decls {
				if strings.HasPrefix(d.prop, "--") {
					out[d.prop] = d.value
				}
			}
		}
		if found != 1 {
			t.Errorf("style.css: %d rules for %s, want exactly 1 palette block", found, selector)
		}
		return out
	}
	return tokens(selLight), tokens(selDark), tokens(selMediaDark)
}

// expectedTokens returns the issue's palette (sections 1 and 2, the openHAB
// colour of Q1) and the tokens the plan adds, for the light or dark mode.
func expectedTokens(dark bool) map[string]string {
	pick := func(light, dk string) string {
		if dark {
			return dk
		}
		return light
	}
	return map[string]string{
		"--bg":        pick("#f3f4f7", "#121418"),
		"--card-bg":   pick("#ffffff", "#1b1e24"),
		"--text":      pick("#171a1f", "#e8eaee"),
		"--muted":     pick("#5f6876", "#a0a7b3"),
		"--border":    pick("#e4e7ec", "#2b2f37"),
		"--track":     pick("#e8eaef", "#2c3039"),
		"--accent":    pick("#2563eb", "#60a5fa"),
		"--on-accent": pick("#ffffff", "#0b1220"),
		"--ok":        pick("#16a34a", "#22c55e"),
		"--ok-ink":    pick("#15803d", "#4ade80"),
		"--warn":      pick("#d97706", "#f59e0b"),
		"--warn-ink":  pick("#92400e", "#fbbf24"),
		"--crit":      pick("#dc2626", "#ef4444"),
		"--crit-ink":  pick("#b91c1c", "#f87171"),
		"--shadow": pick("0 1px 2px rgba(16,24,40,.05), 0 4px 14px rgba(16,24,40,.05)",
			"0 1px 2px rgba(0,0,0,.35)"),

		"--accent-system":      pick("#2563eb", "#60a5fa"),
		"--accent-updates":     pick("#b45309", "#fbbf24"),
		"--accent-uptime":      pick("#0f766e", "#2dd4bf"),
		"--accent-cpu":         pick("#7c3aed", "#a78bfa"),
		"--accent-load":        pick("#15803d", "#4ade80"),
		"--accent-temperature": pick("#c2410c", "#fb923c"),
		"--accent-memory":      pick("#4338ca", "#818cf8"),
		"--accent-disks":       pick("#0e7490", "#22d3ee"),
		"--accent-network":     pick("#be185d", "#f472b6"),
		"--accent-openhab":     pick("#0369a1", "#38bdf8"),

		"--chip-tint":        pick("12%", "16%"),
		"--btn-tint":         pick("8%", "14%"),
		"--btn-border-tint":  pick("30%", "35%"),
		"--spark-fill-alpha": pick("0.28", "0.30"),

		"--dialog-shadow":  "0 12px 40px rgba(0, 0, 0, 0.35)",
		"--backdrop":       "rgba(0, 0, 0, 0.5)",
		"--radius-card":    "14px",
		"--radius-control": "12px",
		"--radius-button":  "10px",
		"--radius-chip":    "8px",
		"--card-padding":   "8px 18px 18px",
		"--gap":            "16px",
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// tokenDiffs lists every expected token that block lacks or holds with
// another value.
func tokenDiffs(name string, block, want map[string]string) []string {
	var out []string
	for _, k := range sortedKeys(want) {
		got, ok := block[k]
		switch {
		case !ok:
			out = append(out, fmt.Sprintf("%s: %s is missing, want %q", name, k, want[k]))
		case normCSS(got) != normCSS(want[k]):
			out = append(out, fmt.Sprintf("%s: %s = %q, want %q", name, k, got, want[k]))
		}
	}
	return out
}

// mapDiffs lists the tokens in which two blocks differ (set and values).
func mapDiffs(a, b map[string]string) []string {
	var out []string
	for _, k := range sortedKeys(a) {
		if bv, ok := b[k]; !ok {
			out = append(out, k+" only in the first block")
		} else if normCSS(a[k]) != normCSS(bv) {
			out = append(out, fmt.Sprintf("%s: %q vs %q", k, a[k], bv))
		}
	}
	for _, k := range sortedKeys(b) {
		if _, ok := a[k]; !ok {
			out = append(out, k+" only in the second block")
		}
	}
	return out
}

func TestStyleCSS_PaletteBlocks_AC2(t *testing.T) {
	css := readAsset(t, "style.css")
	light, dark, media := paletteBlocks(t, css)

	t.Run("AC2 light block holds the palette and the added tokens", func(t *testing.T) {
		for _, d := range tokenDiffs("light block", light, expectedTokens(false)) {
			t.Error(d)
		}
	})
	t.Run("AC2 explicit dark block holds the palette and the added tokens", func(t *testing.T) {
		for _, d := range tokenDiffs(selDark, dark, expectedTokens(true)) {
			t.Error(d)
		}
	})
	t.Run("AC2 OS dark block holds the palette and the added tokens", func(t *testing.T) {
		for _, d := range tokenDiffs(selMediaDark, media, expectedTokens(true)) {
			t.Error(d)
		}
	})
	t.Run("AC2 both dark blocks hold the same tokens with the same values", func(t *testing.T) {
		for _, d := range mapDiffs(dark, media) {
			t.Errorf("dark blocks differ: %s", d)
		}
	})
	t.Run("AC2 --gauge-track is gone", func(t *testing.T) {
		if strings.Contains(css, "--gauge-track") {
			t.Error("style.css: expected no --gauge-track (replaced by --track)")
		}
	})
	t.Run("AC2 shadows, backdrop, radii, padding and gaps are used through tokens", func(t *testing.T) {
		rules := parseCSSRules(css)
		want := []struct{ selector, prop, value string }{
			{".card", "border-radius", "var(--radius-card)"},
			{".card", "padding", "var(--card-padding)"},
			{".card", "box-shadow", "var(--shadow)"},
			{".card", "margin-bottom", "var(--gap)"},
			{"main", "column-gap", "var(--gap)"},
			{".card-icon", "border-radius", "var(--radius-chip)"},
			{"dialog.modal[open]", "box-shadow", "var(--dialog-shadow)"},
			{"dialog.modal::backdrop", "background", "var(--backdrop)"},
		}
		for _, w := range want {
			if got := declsFor(rules, w.selector)[w.prop]; got != w.value {
				t.Errorf("style.css: %s { %s } = %q, want %q", w.selector, w.prop, got, w.value)
			}
		}
	})
}

// colourLiterals returns the colour literals (hex, rgb()/rgba()/hsl()/hsla()
// and friends, named colours) that the declaration values of css outside the
// three palette blocks hold. Named colours match whole value tokens only,
// never a property name such as white-space.
func colourLiterals(css string) []string {
	var found []string
	for _, r := range parseCSSRules(css) {
		if isPaletteRule(r) {
			continue
		}
		sel := strings.Join(r.selectors, ", ")
		for _, d := range r.decls {
			found = append(found, declLiterals(sel, d.prop, d.value)...)
		}
	}
	return found
}

// declLiterals returns the colour literals of one declaration value of the selector sel outside the palette blocks.
func declLiterals(sel, prop, value string) []string {
	var found []string
	v := cssStringRE.ReplaceAllString(cssURLRE.ReplaceAllString(value, ""), "")
	for _, lit := range cssHexRE.FindAllString(v, -1) {
		found = append(found, fmt.Sprintf("%s { %s: %s }: %s", sel, prop, value, lit))
	}
	for _, lit := range cssColourFnRE.FindAllString(v, -1) {
		found = append(found, fmt.Sprintf("%s { %s: %s }: %s", sel, prop, value, lit))
	}
	for _, tok := range cssIdentRE.FindAllString(v, -1) {
		if cssNamedColours[strings.ToLower(tok)] {
			found = append(found, fmt.Sprintf("%s { %s: %s }: %s", sel, prop, value, tok))
		}
	}
	return found
}

func TestStyleCSS_NoColourLiteralOutsidePalette_AC2(t *testing.T) {
	t.Run("AC2 scanner catches literals and spares names and allowed keywords", func(t *testing.T) {
		cases := []struct {
			name, css string
			want      int
		}{
			{"named colour", ".a { color: white; }", 1},
			{"white-space is a property name", ".a { white-space: nowrap; }", 0},
			{"hex", ".a { background: #fff; }", 1},
			{"rgba", ".a { box-shadow: 0 0 1px rgba(0, 0, 0, .5); }", 1},
			{"hsl", ".a { color: hsl(10 20% 30%); }", 1},
			{"allowed keywords", ".a { color: currentColor; background: transparent; border-color: inherit; }", 0},
			{"color-scheme keywords", ":root { color-scheme: light dark; }", 0},
			{"token reference named like a colour", ".a { color: var(--white); }", 0},
			{"inside color-mix", ".a { background: color-mix(in srgb, red 10%, transparent); }", 1},
			{"literal inside the palette block", ":root { --x: #fff; --y: rgba(0,0,0,.5); }", 0},
			{"literal inside the media palette block", "@media (prefers-color-scheme: dark) {\n :root:not([data-theme]) { --x: #fff; }\n}", 0},
			{"literal in a media rule", "@media (max-width: 1px) {\n .a { color: #fff; }\n}", 1},
			{"font names", `.a { font-family: system-ui, "Segoe UI", Roboto, sans-serif; }`, 0},
		}
		for _, c := range cases {
			if got := colourLiterals(c.css); len(got) != c.want {
				t.Errorf("%s: %d literals %v, want %d", c.name, len(got), got, c.want)
			}
		}
	})
	t.Run("AC2 style.css holds no colour literal outside the palette blocks", func(t *testing.T) {
		for _, lit := range colourLiterals(readAsset(t, "style.css")) {
			t.Errorf("style.css: colour literal outside the palette blocks: %s", lit)
		}
	})
}

// colourRule is one expectation of the status-text rules: the merged
// declarations of selector must hold prop with the value.
type colourRule struct{ selector, prop, value string }

func levelRules() []colourRule {
	var out []colourRule
	for _, l := range []string{"ok", "warn", "crit"} {
		ink := "var(--" + l + "-ink)"
		out = append(out,
			colourRule{".metric-" + l, "color", ink},
			colourRule{".kv-val.metric-" + l, "color", ink})
	}
	for _, l := range []string{"warn", "crit"} {
		ink := "var(--" + l + "-ink)"
		out = append(out,
			colourRule{".bar-label .bar-pct.metric-" + l, "color", ink},
			colourRule{".openhab-things summary.metric-" + l, "color", ink},
			colourRule{".alert-banner.metric-" + l, "color", ink},
			colourRule{".alert-banner.metric-" + l, "background", "color-mix(in srgb, var(--" + l + ") 14%, transparent)"},
			colourRule{".alert-banner.metric-" + l, "border-color", "color-mix(in srgb, var(--" + l + ") 35%, transparent)"},
			colourRule{".alert-badge.metric-" + l, "color", ink})
	}
	return append(out,
		colourRule{".stale-badge", "color", "var(--warn-ink)"},
		colourRule{".updates-table td.pkg-new", "color", "var(--ok-ink)"},
		colourRule{".apikey-error", "color", "var(--crit-ink)"},
		colourRule{".card-warn", "border-color", "var(--warn)"},
		// Fills keep the base token.
		colourRule{".bar-fill", "background", "var(--ok)"},
		colourRule{".bar-fill.metric-warn", "background", "var(--warn)"},
		colourRule{".bar-fill.metric-crit", "background", "var(--crit)"},
		// Text on an --accent fill.
		colourRule{".range-button.active", "color", "var(--on-accent)"},
		colourRule{".apikey-submit", "color", "var(--on-accent)"},
		colourRule{".logo-tile", "color", "var(--on-accent)"})
}

func TestStyleCSS_StatusTextUsesInk_AC3(t *testing.T) {
	css := readAsset(t, "style.css")

	t.Run("AC3 no text colour uses a base status token", func(t *testing.T) {
		checkNoBaseTokenText(t, css)
	})
	t.Run("AC3 status text, fills and text on accent follow their rules", func(t *testing.T) {
		checkLevelRules(t, css)
	})
	t.Run("AC3 bar and gauge tracks use --track", func(t *testing.T) {
		checkBarTrackToken(t, css)
	})
}

// checkNoBaseTokenText checks that no text colour in style.css takes a base status token.
func checkNoBaseTokenText(t *testing.T, css string) {
	t.Helper()
	for _, r := range parseCSSRules(css) {
		if isPaletteRule(r) {
			continue
		}
		for _, d := range r.decls {
			if d.prop == "color" && cssBaseTokenRE.MatchString(d.value) {
				t.Errorf("style.css: %s { color: %s }: text takes the *-ink token, fills the base token",
					strings.Join(r.selectors, ", "), d.value)
			}
		}
	}
}

// checkLevelRules checks the status text, fill and accent-text rules of levelRules.
func checkLevelRules(t *testing.T, css string) {
	t.Helper()
	rules := parseCSSRules(css)
	for _, w := range levelRules() {
		if got := declsFor(rules, w.selector)[w.prop]; normCSS(got) != normCSS(w.value) {
			t.Errorf("style.css: %s { %s } = %q, want %q", w.selector, w.prop, got, w.value)
		}
	}
}

// checkBarTrackToken checks that the bar track uses the --track token.
func checkBarTrackToken(t *testing.T, css string) {
	t.Helper()
	rules := parseCSSRules(css)
	if got := declsFor(rules, ".bar-track")["background"]; got != "var(--track)" {
		t.Errorf("style.css: .bar-track { background } = %q, want %q", got, "var(--track)")
	}
}

func TestStyleCSS_CardChips_AC4(t *testing.T) {
	css := readAsset(t, "style.css")

	t.Run("AC4 cards have no coloured top border", func(t *testing.T) {
		checkCardNoTopBorder(t, css)
	})
	t.Run("AC4 every card id maps its colour class to its palette token", func(t *testing.T) {
		checkCardAccentTokens(t, css)
	})
	t.Run("AC4 chip is 28 px with the chip radius and the tinted card colour", func(t *testing.T) {
		checkChipGeometry(t, css)
	})
	t.Run("AC4 the chip icon takes the card colour", func(t *testing.T) {
		checkChipIconColour(t, css)
	})
}

// checkCardNoTopBorder checks that cards and the warning card set no coloured top border.
func checkCardNoTopBorder(t *testing.T, css string) {
	t.Helper()
	rules := parseCSSRules(css)
	for _, sel := range []string{".card", ".card-warn"} {
		for prop := range declsFor(rules, sel) {
			if strings.HasPrefix(prop, "border-top") {
				t.Errorf("style.css: %s sets %s, want no top border", sel, prop)
			}
		}
	}
	for _, r := range rules {
		for _, d := range r.decls {
			if strings.HasPrefix(d.prop, "border-top") && strings.Contains(d.value, "--card-accent") {
				t.Errorf("style.css: %s { %s: %s }: the card colour stays off the border",
					strings.Join(r.selectors, ", "), d.prop, d.value)
			}
		}
	}
}

// checkCardAccentTokens checks that each card id maps its colour class to its palette token.
func checkCardAccentTokens(t *testing.T, css string) {
	t.Helper()
	rules := parseCSSRules(css)
	ids := []string{"system", "updates", "uptime", "cpu", "load", "temperature", "memory", "disks", "network", "openhab"}
	for _, id := range ids {
		sel := ".accent-" + id
		want := "var(--accent-" + id + ")"
		if got := declsFor(rules, sel)["--card-accent"]; got != want {
			t.Errorf("style.css: %s { --card-accent } = %q, want %q", sel, got, want)
		}
	}
}

// checkChipGeometry checks the size, radius and tinted background of the card icon chip.
func checkChipGeometry(t *testing.T, css string) {
	t.Helper()
	rules := parseCSSRules(css)
	chip := declsFor(rules, ".card-icon")
	want := map[string]string{
		"width":         "28px",
		"height":        "28px",
		"border-radius": "var(--radius-chip)",
		"background":    "color-mix(in srgb, var(--card-accent) var(--chip-tint), transparent)",
	}
	for prop, v := range want {
		if normCSS(chip[prop]) != normCSS(v) {
			t.Errorf("style.css: .card-icon { %s } = %q, want %q", prop, chip[prop], v)
		}
	}
}

// checkChipIconColour checks that the chip icon takes the card colour.
func checkChipIconColour(t *testing.T, css string) {
	t.Helper()
	rules := parseCSSRules(css)
	for _, sel := range []string{".card-icon", ".card-icon .icon", ".card-icon svg"} {
		if declsFor(rules, sel)["color"] == "var(--card-accent)" {
			return
		}
	}
	t.Error("style.css: expected .card-icon (or .card-icon .icon / .card-icon svg) { color: var(--card-accent) }")
}

// hexRGB parses #rgb and #rrggbb.
func hexRGB(v string) ([3]float64, error) {
	var rgb [3]float64
	h := strings.TrimPrefix(strings.TrimSpace(v), "#")
	if len(h) == 3 {
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	}
	if len(h) != 6 {
		return rgb, fmt.Errorf("%q is not a hex colour", v)
	}
	for i := range rgb {
		n, err := strconv.ParseUint(h[2*i:2*i+2], 16, 8)
		if err != nil {
			return rgb, fmt.Errorf("%q is not a hex colour: %w", v, err)
		}
		rgb[i] = float64(n)
	}
	return rgb, nil
}

// luminance is the WCAG 2 relative luminance of a colour with 0-255 channels.
func luminance(rgb [3]float64) float64 {
	lin := func(c float64) float64 {
		c /= 255
		if c <= 0.04045 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(rgb[0]) + 0.7152*lin(rgb[1]) + 0.0722*lin(rgb[2])
}

func contrastRatio(a, b [3]float64) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// mixRGB is color-mix(in srgb, fg pct%, transparent) composited over bg.
func mixRGB(fg, bg [3]float64, pct float64) [3]float64 {
	var out [3]float64
	for i := range out {
		out[i] = fg[i]*pct + bg[i]*(1-pct)
	}
	return out
}

// contrastFailures checks the AC12 pairs of one mode's tokens and returns a
// message for each pair that is missing, unparsable or below 4.5:1.
func contrastFailures(mode string, tokens map[string]string) []string {
	var out []string
	colour := func(name string) ([3]float64, bool) {
		v, ok := tokens[name]
		if !ok {
			out = append(out, fmt.Sprintf("%s: %s is missing", mode, name))
			return [3]float64{}, false
		}
		rgb, err := hexRGB(v)
		if err != nil {
			out = append(out, fmt.Sprintf("%s: %s: %v", mode, name, err))
			return rgb, false
		}
		return rgb, true
	}
	check := func(fgName string, fg [3]float64, bgName string, bg [3]float64) {
		if r := contrastRatio(fg, bg); r < 4.5 {
			out = append(out, fmt.Sprintf("%s: %s on %s = %.2f:1, want at least 4.5:1", mode, fgName, bgName, r))
		}
	}
	pair := func(fgName, bgName string) {
		fg, okFg := colour(fgName)
		bg, okBg := colour(bgName)
		if okFg && okBg {
			check(fgName, fg, bgName, bg)
		}
	}
	for _, fg := range []string{"--text", "--muted", "--ok-ink", "--warn-ink", "--crit-ink"} {
		pair(fg, "--card-bg")
	}
	pair("--text", "--bg")
	pair("--muted", "--bg")
	pair("--on-accent", "--accent")

	accent, okAccent := colour("--accent")
	card, okCard := colour("--card-bg")
	tint, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(tokens["--btn-tint"]), "%"), 64)
	switch {
	case !okAccent || !okCard:
	case err != nil:
		out = append(out, fmt.Sprintf("%s: --btn-tint = %q is not a percentage", mode, tokens["--btn-tint"]))
	default:
		check("--accent label", accent, fmt.Sprintf("--accent at %s over --card-bg", tokens["--btn-tint"]), mixRGB(accent, card, tint/100))
	}
	return out
}

func TestStyleCSS_Contrast_AC12(t *testing.T) {
	t.Run("AC12 ratio helper matches known WCAG values", func(t *testing.T) {
		checkRatioHelper(t)
	})
	t.Run("AC12 a pair below 4.5 is reported", func(t *testing.T) {
		checkFailureReported(t)
	})
	t.Run("AC12 the issue's palette meets 4.5:1 in both modes", func(t *testing.T) {
		checkPaletteMeetsContrast(t)
	})

	light, dark, media := paletteBlocks(t, readAsset(t, "style.css"))
	for _, mode := range []struct {
		name   string
		tokens map[string]string
	}{{"light", light}, {"dark", dark}, {"OS dark", media}} {
		t.Run("AC12 style.css tokens reach 4.5:1 in "+mode.name+" mode", func(t *testing.T) {
			checkModeContrast(t, mode.name, mode.tokens)
		})
	}
}

// checkRatioHelper checks contrastRatio against known WCAG values.
func checkRatioHelper(t *testing.T) {
	t.Helper()
	black, _ := hexRGB("#000")
	white, _ := hexRGB("#ffffff")
	grey, _ := hexRGB("#767676")
	if got := contrastRatio(black, white); math.Abs(got-21) > 0.01 {
		t.Errorf("black on white = %.3f, want 21", got)
	}
	if got := contrastRatio(grey, white); got < 4.5 || got > 4.6 {
		t.Errorf("#767676 on white = %.3f, want about 4.54", got)
	}
	green, _ := hexRGB("#16a34a")
	if got := contrastRatio(green, white); got > 3.4 {
		t.Errorf("#16a34a on white = %.3f, want about 3.3 (the value that motivates --ok-ink)", got)
	}
}

// checkFailureReported checks that a pair below 4.5:1 is reported.
func checkFailureReported(t *testing.T) {
	t.Helper()
	tokens := expectedTokens(false)
	tokens["--ok-ink"] = "#16a34a"
	if got := contrastFailures("light", tokens); len(got) != 1 {
		t.Errorf("failures = %v, want exactly the --ok-ink pair", got)
	}
}

// checkPaletteMeetsContrast checks that the issue's palette meets 4.5:1 in both modes.
func checkPaletteMeetsContrast(t *testing.T) {
	t.Helper()
	for _, dark := range []bool{false, true} {
		if got := contrastFailures(fmt.Sprintf("dark=%v", dark), expectedTokens(dark)); len(got) != 0 {
			t.Errorf("failures = %v, want none", got)
		}
	}
}

// checkModeContrast reports every contrast failure of one mode's tokens.
func checkModeContrast(t *testing.T, mode string, tokens map[string]string) {
	t.Helper()
	for _, f := range contrastFailures(mode, tokens) {
		t.Error(f)
	}
}
