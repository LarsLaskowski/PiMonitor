package web

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

// This file guards the theme selection of issue #218: the registry in
// theme-init.js, the stored choice, the picker of the layout dialog, the
// per-theme token blocks of style.css and the readers of the canvas
// renderers. Like the other tests of this package it reads the embedded assets
// as text; the runtime behaviour is covered by the manual check of the plan.

type themeEntry struct{ id, name string }

var (
	themeRegistryRE = regexp.MustCompile(`(?s)const PIMONITOR_THEMES = Object\.freeze\(\[(.*?)\]\);`)
	themeEntryRE    = regexp.MustCompile(`Object\.freeze\(\{\s*id:\s*'([^']*)'\s*,\s*name:\s*'([^']*)'\s*\}\)`)
	themeFallbackRE = regexp.MustCompile(`(?s)const DRAWING_FALLBACKS = Object\.freeze\(\{(.*?)\}\);`)
	themeFallbackKV = regexp.MustCompile(`'(--[a-z-]+)'\s*:\s*'([^']*)'`)
	themeReaderRE   = regexp.MustCompile(`theme(?:Color|Number)\(\s*'(--[a-z-]+)'`)
	themeReturnRE   = regexp.MustCompile(`return '(--[a-z-]+)'`)
	themeColorsRE   = regexp.MustCompile(`'openhab_[a-z_]+'\s*:\s*'(--[a-z-]+)'`)
	themeEntryLitRE = regexp.MustCompile(`\{\s*id:\s*'[^']*'\s*,\s*name:`)
	storageKeyRE    = regexp.MustCompile(`['"](pimonitor-[a-z-]+)['"]`)
	ariaLabelHTMLRE = regexp.MustCompile(`aria-label="([^"]*)"`)
	linkTagRE       = regexp.MustCompile(`<link\b[^>]*>`)
	scriptHeadRE    = regexp.MustCompile(`<script\b[^>]*\bsrc="theme-init\.js"[^>]*>`)
)

var storageKeys = []string{
	"pimonitor-api-key", "pimonitor-theme", "pimonitor-style", "pimonitor-layout", "pimonitor-openhab-things",
}

// parseThemeRegistry returns the entries of the PIMONITOR_THEMES array of src.
func parseThemeRegistry(src string) []themeEntry {
	m := themeRegistryRE.FindStringSubmatch(src)
	if m == nil {
		return nil
	}
	var out []themeEntry
	for _, e := range themeEntryRE.FindAllStringSubmatch(m[1], -1) {
		out = append(out, themeEntry{e[1], e[2]})
	}
	return out
}

// parseDrawingFallbacks returns the property -> value map of DRAWING_FALLBACKS.
func parseDrawingFallbacks(src string) map[string]string {
	out := map[string]string{}
	if m := themeFallbackRE.FindStringSubmatch(src); m != nil {
		for _, kv := range themeFallbackKV.FindAllStringSubmatch(m[1], -1) {
			out[kv[1]] = kv[2]
		}
	}
	return out
}

// themeInitIIFE returns the body of the self-invoking function of theme-init.js.
func themeInitIIFE(src string) string {
	return jsFunctionBody(src, "(function ()")
}

// drawingReads returns the sorted custom properties the canvas renderers read:
// the literal arguments of themeColor/themeNumber in chart.js, gauge.js and
// app.js, the returns of gaugeColorVar and the values of the openHAB colors map.
func drawingReads(t *testing.T) []string {
	t.Helper()
	set := map[string]bool{}
	add := func(re *regexp.Regexp, src string) {
		for _, m := range re.FindAllStringSubmatch(src, -1) {
			set[m[1]] = true
		}
	}
	gauge := readAsset(t, "gauge.js")
	app := readAsset(t, "app.js")
	for _, src := range []string{readAsset(t, "chart.js"), gauge, app} {
		add(themeReaderRE, src)
	}
	add(themeReturnRE, jsFunctionBody(gauge, "function gaugeColorVar("))
	add(themeColorsRE, app)
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// themeSelectors returns the selectors of a theme's light, explicit dark and
// OS-dark blocks; the default theme (Clear) is unscoped.
func themeSelectors(id string, isDefault bool) (light, dark, media string) {
	if isDefault {
		return selLight, selDark, selMediaDark
	}
	base := fmt.Sprintf(`:root[data-style="%s"]`, id)
	return base, base + `[data-theme="dark"]`, base + ":not([data-theme])"
}

// blockTokens returns the custom properties of the rules whose selector list
// holds selector and how many such rules there are.
func blockTokens(rules []cssRule, selector string) (map[string]string, int) {
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
	return out, found
}

// keySetDiff names the keys of want that got lacks and the keys of got that want lacks.
func keySetDiff(got, want map[string]string) []string {
	var out []string
	for _, k := range sortedKeys(want) {
		if _, ok := got[k]; !ok {
			out = append(out, "lacks "+k)
		}
	}
	for _, k := range sortedKeys(got) {
		if _, ok := want[k]; !ok {
			out = append(out, "has extra "+k)
		}
	}
	return out
}

// themeBlockProblems reports every way the palette blocks of the registered
// ids deviate from the contract: exactly one rule per selector, the OS-dark
// block inside the prefers-color-scheme media query, one key set across a
// theme's three blocks and every property in reads defined by each theme.
func themeBlockProblems(css string, ids, reads []string) []string {
	var problems []string
	plain := cssCommentRE.ReplaceAllString(css, "")
	rules := parseCSSRules(css)
	for i, id := range ids {
		problems = append(problems, themeBlockProblemsFor(plain, rules, id, i == 0, reads)...)
	}
	return problems
}

// themeBlockProblemsFor checks the three palette blocks of one theme.
func themeBlockProblemsFor(plain string, rules []cssRule, id string, isDefault bool, reads []string) []string {
	var problems []string
	lightSel, darkSel, mediaSel := themeSelectors(id, isDefault)
	mediaRE := regexp.MustCompile(`@media\s*\(\s*prefers-color-scheme:\s*dark\s*\)\s*\{\s*` + regexp.QuoteMeta(mediaSel) + `\s*\{`)
	if !mediaRE.MatchString(plain) {
		problems = append(problems, fmt.Sprintf("%s: no %s block inside the prefers-color-scheme: dark media query", id, mediaSel))
	}
	light, nl := blockTokens(rules, lightSel)
	dark, nd := blockTokens(rules, darkSel)
	media, nm := blockTokens(rules, mediaSel)
	for sel, n := range map[string]int{lightSel: nl, darkSel: nd, mediaSel: nm} {
		if n != 1 {
			problems = append(problems, fmt.Sprintf("%s: %d rules for %s, want exactly 1", id, n, sel))
		}
	}
	for _, d := range keySetDiff(dark, light) {
		problems = append(problems, fmt.Sprintf("%s: dark block %s compared with the light block", id, d))
	}
	for _, d := range keySetDiff(media, light) {
		problems = append(problems, fmt.Sprintf("%s: OS-dark block %s compared with the light block", id, d))
	}
	for _, p := range reads {
		if _, ok := light[p]; !ok {
			problems = append(problems, fmt.Sprintf("%s: light block lacks %s read by a canvas renderer", id, p))
		}
	}
	return problems
}

// themeFixtureCSS builds a stylesheet with Clear's three blocks and one block
// set per extra id; a token listed in skip is left out of the extra theme's
// light block.
func themeFixtureCSS(tokens []string, extra []string, skip string) string {
	decls := func(omit string) string {
		var b strings.Builder
		for _, k := range tokens {
			if k != omit {
				b.WriteString(k + ": #000;\n")
			}
		}
		return b.String()
	}
	var b strings.Builder
	b.WriteString(":root {\n" + decls("") + "}\n:root[data-theme=\"dark\"] {\n" + decls("") + "}\n")
	b.WriteString("@media (prefers-color-scheme: dark) {\n  :root:not([data-theme]) {\n" + decls("") + "}\n}\n")
	for _, id := range extra {
		base := fmt.Sprintf(`:root[data-style="%s"]`, id)
		b.WriteString(base + " {\n" + decls(skip) + "}\n")
		b.WriteString(base + "[data-theme=\"dark\"] {\n" + decls("") + "}\n")
		b.WriteString("@media (prefers-color-scheme: dark) {\n  " + base + ":not([data-theme]) {\n" + decls("") + "}\n}\n")
	}
	return b.String()
}

func TestThemeInit_DefaultLeavesNoAttribute_AC1(t *testing.T) {
	src := readAsset(t, "theme-init.js")
	html := readAsset(t, "index.html")

	t.Run("AC1 dataset.style is set only for a registered id other than the first", func(t *testing.T) {
		iife := themeInitIIFE(src)
		requireContains(t, "theme-init.js IIFE", iife, "'pimonitor-style'", "PIMONITOR_THEMES[0].id", "dataset.style = ")
		if strings.Count(iife, "dataset.style") != 1 {
			t.Errorf("theme-init.js IIFE: want exactly one write of dataset.style, got %d mentions", strings.Count(iife, "dataset.style"))
		}
	})

	t.Run("AC1 theme-init.js never writes or removes storage", func(t *testing.T) {
		for _, bad := range []string{"setItem", "removeItem", "localStorage.clear"} {
			if strings.Contains(src, bad) {
				t.Errorf("theme-init.js: expected no %s, the stored choice is only read", bad)
			}
		}
	})

	t.Run("AC1 the html element carries no data-style", func(t *testing.T) {
		m := regexp.MustCompile(`<html\b[^>]*>`).FindString(html)
		if m == "" || strings.Contains(m, "data-style") {
			t.Errorf("index.html: <html> tag = %q, want one without data-style", m)
		}
	})
}

// checkThemeInitSync checks that theme-init.js loads synchronously after style.css and before <body.
func checkThemeInitSync(t *testing.T, html string) {
	t.Helper()
	tag := scriptHeadRE.FindString(html)
	if tag == "" {
		t.Fatal("index.html: expected a <script src=\"theme-init.js\"> tag")
	}
	for _, bad := range []string{"defer", "async", "module"} {
		if strings.Contains(tag, bad) {
			t.Errorf("index.html: %s has %q, want a synchronous script", tag, bad)
		}
	}
	at, css, body := strings.Index(html, tag), strings.Index(html, `href="style.css"`), strings.Index(html, "<body")
	if css < 0 || at < css || body < at {
		t.Errorf("index.html: style.css at %d, theme-init.js at %d, <body at %d, want that order", css, at, body)
	}
}

func TestThemeInit_AppliesStoredThemeBeforePaint_AC2(t *testing.T) {
	src := readAsset(t, "theme-init.js")
	html := readAsset(t, "index.html")

	t.Run("AC2 the stored theme is read in its own try after the mode", func(t *testing.T) {
		iife := themeInitIIFE(src)
		if n := strings.Count(iife, "try {"); n != 2 {
			t.Errorf("theme-init.js IIFE: %d try blocks, want 2 (mode, then theme)", n)
		}
		mode, style := strings.Index(iife, "'pimonitor-theme'"), strings.Index(iife, "'pimonitor-style'")
		if mode < 0 || style < mode {
			t.Errorf("theme-init.js IIFE: pimonitor-style read at %d, want it after the mode read at %d", style, mode)
		}
		requireContains(t, "theme-init.js IIFE", iife, "localStorage.getItem('pimonitor-style')")
	})

	t.Run("AC2 theme-init.js is a synchronous script in head before body", func(t *testing.T) {
		checkThemeInitSync(t, html)
	})

	t.Run("AC2 the registry parser reads a two-entry fixture", func(t *testing.T) {
		fixture := "const PIMONITOR_THEMES = Object.freeze([\n" +
			"  Object.freeze({ id: 'clear', name: 'Clear' }),\n" +
			"  Object.freeze({ id: 'probe', name: 'Probe' }),\n]);\n"
		want := []themeEntry{{"clear", "Clear"}, {"probe", "Probe"}}
		if got := parseThemeRegistry(fixture); !slices.Equal(got, want) {
			t.Errorf("parseThemeRegistry(fixture) = %v, want %v", got, want)
		}
	})
}

func TestThemeInit_UnknownValueIsKept_AC3(t *testing.T) {
	src := readAsset(t, "theme-init.js")
	iife := themeInitIIFE(src)

	t.Run("AC3 the stored value is compared exactly against the registry", func(t *testing.T) {
		requireContains(t, "theme-init.js IIFE", iife, "=== ", "PIMONITOR_THEMES")
		for _, bad := range []string{"toLowerCase", "toUpperCase", ".trim(", "startsWith", "indexOf", "removeItem"} {
			if strings.Contains(iife, bad) {
				t.Errorf("theme-init.js IIFE: unexpected %s, a stored value is matched exactly and never rewritten", bad)
			}
		}
	})

	t.Run("AC3 nothing removes the stored theme", func(t *testing.T) {
		for _, name := range []string{"theme-init.js", "app.js", "chart.js", "gauge.js"} {
			src := readAsset(t, name)
			if strings.Contains(src, "removeItem(STYLE_KEY") || strings.Contains(src, "removeItem('pimonitor-style'") {
				t.Errorf("%s: expected no removal of the stored theme", name)
			}
		}
	})
}

func TestAppJS_StylePickerFromRegistry_AC4(t *testing.T) {
	js := readAsset(t, "app.js")
	html := readAsset(t, "index.html")

	t.Run("AC4 buildStylePicker lists the registry and checks the active theme", func(t *testing.T) {
		requireContains(t, "buildStylePicker", jsFunctionBody(js, "function buildStylePicker("),
			"PIMONITOR_THEMES", "activeStyle()", "checked", "textContent")
	})

	t.Run("AC4 the picker is rebuilt when the layout dialog opens", func(t *testing.T) {
		requireContains(t, "openLayoutModal", jsFunctionBody(js, "function openLayoutModal("), "buildStylePicker()")
	})

	t.Run("AC4 the picker comes before the layout hint", func(t *testing.T) {
		picker, hint := strings.Index(html, `id="style-picker"`), strings.Index(html, `class="layout-hint"`)
		if picker < 0 || hint < 0 || picker > hint {
			t.Errorf("index.html: #style-picker at %d, .layout-hint at %d, want the picker first", picker, hint)
		}
	})

	t.Run("AC4 activeStyle falls back to the first registered theme", func(t *testing.T) {
		requireContains(t, "activeStyle", jsFunctionBody(js, "function activeStyle("), "dataset.style", "PIMONITOR_THEMES")
	})
}

func TestAppJS_SelectStyleAppliesAndStores_AC5(t *testing.T) {
	js := readAsset(t, "app.js")

	t.Run("AC5 selectStyle stores the id and then applies it", func(t *testing.T) {
		body := jsFunctionBody(js, "function selectStyle(")
		requireContains(t, "selectStyle", body, "setItem(STYLE_KEY,", "applyStyle(")
		if store, apply := strings.Index(body, "setItem(STYLE_KEY,"), strings.Index(body, "applyStyle("); store < 0 || apply < store {
			t.Errorf("selectStyle: setItem at %d, applyStyle at %d, want storing first", store, apply)
		}
	})

	t.Run("AC5 applyStyle repaints the canvases", func(t *testing.T) {
		requireContains(t, "applyStyle", jsFunctionBody(js, "function applyStyle("), "repaintCanvases()")
	})

	t.Run("AC5 the dialog stays open while a theme is chosen", func(t *testing.T) {
		for _, fn := range []string{"function selectStyle(", "function applyStyle(", "function buildStylePicker("} {
			body := jsFunctionBody(js, fn)
			if strings.TrimSpace(body) == "" {
				t.Errorf("app.js: expected a non-empty %s", fn)
			}
			if strings.Contains(body, ".close()") {
				t.Errorf("%s: expected no .close(), the dialog stays open", fn)
			}
		}
	})

	t.Run("AC5 applyTheme redraws through repaintCanvases", func(t *testing.T) {
		checkApplyThemeRedraws(t, js)
	})
}

func TestStylePicker_HiddenWithOneTheme_AC6(t *testing.T) {
	html := readAsset(t, "index.html")
	js := readAsset(t, "app.js")

	t.Run("AC6 the picker is statically hidden by the hidden class", func(t *testing.T) {
		requireContains(t, "index.html", html, `<fieldset class="style-picker hidden" id="style-picker">`)
	})

	t.Run("AC6 buildStylePicker toggles the class from the registry size", func(t *testing.T) {
		requireContains(t, "buildStylePicker", jsFunctionBody(js, "function buildStylePicker("),
			"classList.toggle('hidden', PIMONITOR_THEMES.length < 2)")
	})
}

func TestAppJS_ModeAndThemeIndependent_AC7(t *testing.T) {
	js := readAsset(t, "app.js")

	t.Run("AC7 applyStyle never writes the mode", func(t *testing.T) {
		body := jsFunctionBody(js, "function applyStyle(")
		requireContains(t, "applyStyle", body, "dataset.style")
		if strings.Contains(body, "dataset.theme") {
			t.Error("applyStyle: expected no dataset.theme write")
		}
	})

	t.Run("AC7 the mode functions never write the theme", func(t *testing.T) {
		for _, fn := range []string{"function applyTheme(", "function toggleTheme("} {
			body := jsFunctionBody(js, fn)
			if strings.TrimSpace(body) == "" {
				t.Errorf("app.js: expected a non-empty %s", fn)
			}
			if strings.Contains(body, "dataset.style") || strings.Contains(body, "STYLE_KEY") {
				t.Errorf("%s: expected no access to the theme", fn)
			}
		}
	})
}

func TestAppJS_StyleStorageFailureIsCaught_AC8(t *testing.T) {
	js := readAsset(t, "app.js")

	t.Run("AC8 selectStyle writes storage inside try/catch", func(t *testing.T) {
		body := jsFunctionBody(js, "function selectStyle(")
		try, set, catch := strings.Index(body, "try {"), strings.Index(body, "setItem(STYLE_KEY,"), strings.Index(body, "catch (")
		if try < 0 || set < try || catch < set {
			t.Errorf("selectStyle: try at %d, setItem at %d, catch at %d, want that order", try, set, catch)
		}
	})

	t.Run("AC8 activeStyle reads the attribute, not storage", func(t *testing.T) {
		body := jsFunctionBody(js, "function activeStyle(")
		requireContains(t, "activeStyle", body, "dataset.style")
		if strings.Contains(body, "localStorage") {
			t.Error("activeStyle: expected no storage read")
		}
	})
}

// registryProblems reports an empty registry, a default other than clear, and duplicate or empty entries.
func registryProblems(themes []themeEntry) []string {
	if len(themes) == 0 || themes[0].id != "clear" {
		return []string{fmt.Sprintf("theme-init.js registry = %v, want clear first", themes)}
	}
	var out []string
	seen := map[string]bool{}
	for _, th := range themes {
		if seen[th.id] || th.id == "" || th.name == "" {
			out = append(out, fmt.Sprintf("theme-init.js registry entry %v: duplicate or empty id/name", th))
		}
		seen[th.id] = true
	}
	return out
}

// registryCopyProblems reports a registry entry or a registered id literal in src.
func registryCopyProblems(src string, themes []themeEntry) []string {
	var out []string
	if themeEntryLitRE.MatchString(src) {
		out = append(out, "app.js: expected no { id: '…', name: … } entry, the registry lives in theme-init.js")
	}
	for _, th := range themes {
		for _, lit := range []string{"'" + th.id + "'", `"` + th.id + `"`} {
			if strings.Contains(src, lit) {
				out = append(out, fmt.Sprintf("app.js: expected no literal %s of a registered id", lit))
			}
		}
	}
	return out
}

func TestThemeRegistry_SingleSource_AC9(t *testing.T) {
	initJS := readAsset(t, "theme-init.js")
	app := readAsset(t, "app.js")
	themes := parseThemeRegistry(initJS)

	t.Run("AC9 ids are unique and the default comes first", func(t *testing.T) {
		for _, p := range registryProblems(themes) {
			t.Error(p)
		}
	})

	t.Run("AC9 app.js holds no copy of the registry", func(t *testing.T) {
		for _, p := range registryCopyProblems(app, themes) {
			t.Error(p)
		}
	})

	t.Run("AC9 the registry parser reads a two-entry fixture", func(t *testing.T) {
		fixture := "const PIMONITOR_THEMES = Object.freeze([\n" +
			"  Object.freeze({ id: 'clear', name: 'Clear' }),\n" +
			"  Object.freeze({ id: 'probe', name: 'Probe' }),\n]);\n"
		if got := parseThemeRegistry(fixture); len(got) != 2 || got[1].id != "probe" {
			t.Errorf("parseThemeRegistry(fixture) = %v, want clear and probe", got)
		}
		if !themeEntryLitRE.MatchString("{ id: 'probe', name: 'Probe' }") {
			t.Error("themeEntryLitRE: expected it to match a registry entry literal")
		}
	})
}

func TestStyleCSS_ThemeTokenSets_AC10(t *testing.T) {
	css := readAsset(t, "style.css")
	themes := parseThemeRegistry(readAsset(t, "theme-init.js"))
	reads := drawingReads(t)
	var ids []string
	for _, th := range themes {
		ids = append(ids, th.id)
	}

	t.Run("AC10 every registered theme has one token set and defines every property read", func(t *testing.T) {
		if len(reads) == 0 {
			t.Fatal("chart.js, gauge.js, app.js: expected reads through themeColor/themeNumber, found none")
		}
		for _, p := range themeBlockProblems(css, ids, reads) {
			t.Error(p)
		}
	})

	tokens := []string{"--bg", "--accent", "--track"}
	t.Run("AC10 the checker accepts a complete probe theme", func(t *testing.T) {
		fixture := themeFixtureCSS(tokens, []string{"probe"}, "")
		if got := themeBlockProblems(fixture, []string{"clear", "probe"}, tokens); len(got) != 0 {
			t.Errorf("themeBlockProblems(complete fixture) = %v, want none", got)
		}
	})

	t.Run("AC10 the checker reports a probe theme missing a token", func(t *testing.T) {
		fixture := themeFixtureCSS(tokens, []string{"probe"}, "--track")
		got := strings.Join(themeBlockProblems(fixture, []string{"clear", "probe"}, tokens), "\n")
		if !strings.Contains(got, "probe: dark block has extra --track") || !strings.Contains(got, "probe: light block lacks --track") {
			t.Errorf("themeBlockProblems(fixture missing --track) = %q, want the missing token named for probe", got)
		}
	})

	t.Run("AC10 the extractor finds the non-literal reads", func(t *testing.T) {
		checkReadExtractors(t)
	})
}

// checkReadExtractors runs the non-literal read patterns on fixture strings.
func checkReadExtractors(t *testing.T) {
	t.Helper()
	gauge := jsFunctionBody("function gaugeColorVar(c) { if (c) return '--crit'; return '--ok'; }", "function gaugeColorVar(")
	if got := themeReturnRE.FindAllStringSubmatch(gauge, -1); len(got) != 2 || got[0][1] != "--crit" || got[1][1] != "--ok" {
		t.Errorf("gaugeColorVar returns = %v, want --crit and --ok", got)
	}
	if m := themeColorsRE.FindStringSubmatch("{ 'openhab_heap_used_bytes': '--muted' }"); m == nil || m[1] != "--muted" {
		t.Errorf("colors map match = %v, want --muted", m)
	}
}

func TestIndexHTML_StylePickerIsRadioGroup_AC11(t *testing.T) {
	html := readAsset(t, "index.html")
	js := readAsset(t, "app.js")

	t.Run("AC11 the picker is a fieldset with the legend Theme", func(t *testing.T) {
		requireContains(t, "index.html", html, `<fieldset class="style-picker hidden" id="style-picker"><legend>Theme</legend>`)
	})

	t.Run("AC11 buildStylePicker creates labelled radio inputs of one group", func(t *testing.T) {
		requireContains(t, "buildStylePicker", jsFunctionBody(js, "function buildStylePicker("),
			"createElement('label')", "createElement('input')", "type = 'radio'", "name = 'style-choice'", "selectStyle(")
	})
}

func TestAssets_StorageKeys_AC12(t *testing.T) {
	entries, err := assetsFS.ReadDir("assets")
	if err != nil {
		t.Fatalf("read assets: %v", err)
	}
	found := map[string]bool{}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".js") {
			continue
		}
		for _, m := range storageKeyRE.FindAllStringSubmatch(readAsset(t, e.Name()), -1) {
			found[m[1]] = true
			if !slices.Contains(storageKeys, m[1]) {
				t.Errorf("%s: storage key %q is not one of %v", e.Name(), m[1], storageKeys)
			}
		}
	}
	for _, k := range storageKeys {
		if !found[k] {
			t.Errorf("assets: expected the storage key %q in a JS asset", k)
		}
	}
}

func TestAppJS_ResetLayoutKeepsTheme_AC13(t *testing.T) {
	body := jsFunctionBody(readAsset(t, "app.js"), "function resetLayout(")
	if strings.TrimSpace(body) == "" {
		t.Fatal("app.js: expected a function resetLayout()")
	}
	for _, bad := range []string{"STYLE_KEY", "applyStyle", "selectStyle", "dataset.style"} {
		if strings.Contains(body, bad) {
			t.Errorf("resetLayout: unexpected %s, resetting the layout leaves the theme alone", bad)
		}
	}
}

func TestIndexHTML_ModeButtonLabel_AC15(t *testing.T) {
	html := readAsset(t, "index.html")
	app := readAsset(t, "app.js")

	t.Run("AC15 the mode button is labelled Toggle mode", func(t *testing.T) {
		requireContains(t, "index.html", html, `id="theme-toggle" aria-label="Toggle mode"`)
	})

	t.Run("AC15 no accessible label calls the mode a theme", func(t *testing.T) {
		for _, m := range ariaLabelHTMLRE.FindAllStringSubmatch(html, -1) {
			if strings.Contains(strings.ToLower(m[1]), "theme") {
				t.Errorf("index.html: aria-label %q mentions theme, want mode", m[1])
			}
		}
		for i, line := range strings.Split(app, "\n") {
			if strings.Contains(line, "aria-label") && strings.Contains(strings.ToLower(line), "theme") {
				t.Errorf("app.js:%d: aria-label line %q mentions theme, want mode", i+1, strings.TrimSpace(line))
			}
		}
	})
}

func TestIndexHTML_ThemesLoadedStatically_AC16(t *testing.T) {
	html := readAsset(t, "index.html")

	t.Run("AC16 only style.css is linked as a stylesheet", func(t *testing.T) {
		if sheets := stylesheetLinks(html); len(sheets) != 1 || !strings.Contains(sheets[0], `href="style.css"`) {
			t.Errorf("index.html: stylesheet links = %v, want only style.css", sheets)
		}
	})

	t.Run("AC16 no script creates a stylesheet", func(t *testing.T) {
		for _, name := range []string{"app.js", "theme-init.js", "chart.js", "gauge.js"} {
			for _, bad := range scriptStylesheetCreation(readAsset(t, name)) {
				t.Errorf("%s: unexpected %s, every theme ships in style.css", name, bad)
			}
		}
	})
}

// stylesheetLinks returns the <link rel="stylesheet"> tags of html.
func stylesheetLinks(html string) []string {
	var sheets []string
	for _, l := range linkTagRE.FindAllString(html, -1) {
		if strings.Contains(l, `rel="stylesheet"`) {
			sheets = append(sheets, l)
		}
	}
	return sheets
}

// scriptStylesheetCreation returns the ways src could add a stylesheet at runtime.
func scriptStylesheetCreation(src string) []string {
	var found []string
	for _, bad := range []string{"createElement('link')", `createElement("link")`, "insertRule", "adoptedStyleSheets", "createElement('style')"} {
		if strings.Contains(src, bad) {
			found = append(found, bad)
		}
	}
	return found
}

func TestThemeInit_DrawingFallbacks_AC17(t *testing.T) {
	initJS := readAsset(t, "theme-init.js")
	css := readAsset(t, "style.css")
	fallbacks := parseDrawingFallbacks(initJS)

	t.Run("AC17 the fallbacks cover exactly the properties read", func(t *testing.T) {
		reads := drawingReads(t)
		keys := sortedKeys(fallbacks)
		if !slices.Equal(keys, reads) {
			t.Errorf("DRAWING_FALLBACKS keys = %v, want the properties read %v", keys, reads)
		}
	})

	t.Run("AC17 each fallback equals Clear's light value", func(t *testing.T) {
		light, _, _ := paletteBlocks(t, css)
		for _, k := range sortedKeys(fallbacks) {
			if normCSS(fallbacks[k]) != normCSS(light[k]) {
				t.Errorf("DRAWING_FALLBACKS[%s] = %q, want Clear's light value %q", k, fallbacks[k], light[k])
			}
		}
	})

	t.Run("AC17 themeNumber validates finiteness and range without a default", func(t *testing.T) {
		body := jsFunctionBody(initJS, "function themeNumber(")
		requireContains(t, "themeNumber", body, "Number.isFinite(", "min", "max")
		if strings.Contains(body, "||") {
			t.Error("themeNumber: expected no || default, an invalid value falls back to Clear's")
		}
	})

	t.Run("AC17 themeColor validates the color", func(t *testing.T) {
		requireContains(t, "themeColor", jsFunctionBody(initJS, "function themeColor("), "CSS.supports(", "DRAWING_FALLBACKS")
	})

	t.Run("AC17 the renderers read only through themeColor and themeNumber", func(t *testing.T) {
		for _, name := range []string{"chart.js", "gauge.js", "app.js"} {
			if strings.Contains(readAsset(t, name), "getPropertyValue(") {
				t.Errorf("%s: unexpected getPropertyValue(, read through themeColor/themeNumber", name)
			}
		}
	})
}
