package web

import (
	"regexp"
	"strings"
	"testing"
)

// The host name (issue #179) is shown in the header, the tab title and the
// System card. There is no JS test runner (docs/UNIT_TESTS.md), so these
// guards scan the committed assets for the markup, script and styles the
// plan fixes literally.

const hostnameHeaderH1 = `<h1>🖥️ PiMonitor<span class="header-hostname hidden" id="header-hostname">` +
	`(<span class="header-hostname-name" id="header-hostname-name"></span>)</span></h1>`

const hostnameSystemRow = `<div class="kv-row hidden" id="sys-hostname-row"><span class="kv-key">Hostname</span>` +
	`<span class="kv-val" id="sys-hostname"></span></div>`

// readAsset returns an embedded asset with \r\n normalized to \n.
func readAsset(t *testing.T, name string) string {
	t.Helper()
	data, err := assetsFS.ReadFile("assets/" + name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return strings.ReplaceAll(string(data), "\r\n", "\n")
}

// jsFunctionBody returns the text between the braces of the first function
// whose declaration starts with signature, or "" when there is none.
func jsFunctionBody(src, signature string) string {
	start := strings.Index(src, signature)
	if start < 0 {
		return ""
	}
	open := strings.Index(src[start:], "{")
	if open < 0 {
		return ""
	}
	open += start
	depth := 0
	for i := open; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[open+1 : i]
			}
		}
	}
	return ""
}

// requireHostnameRenderer returns the body of renderHostname, failing the
// test when app.js does not define it.
func requireHostnameRenderer(t *testing.T, js string) string {
	t.Helper()
	body := jsFunctionBody(js, "function renderHostname(name)")
	if strings.TrimSpace(body) == "" {
		t.Fatal("app.js: expected a non-empty function renderHostname(name)")
	}
	return body
}

// cssDeclarations returns the declarations of every top-level rule whose
// selector is exactly selector, later declarations overriding earlier ones.
func cssDeclarations(css, selector string) map[string]string {
	decls := map[string]string{}
	rule := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(selector) + `\s*\{([^}]*)\}`)
	for _, m := range rule.FindAllStringSubmatch(css, -1) {
		addDeclarations(decls, m[1])
	}
	return decls
}

func addDeclarations(decls map[string]string, block string) {
	for _, decl := range strings.Split(block, ";") {
		prop, value, ok := strings.Cut(decl, ":")
		if ok {
			decls[strings.TrimSpace(prop)] = strings.Join(strings.Fields(value), " ")
		}
	}
}

// requireCSS fails for every property of want that the top-level rule for
// selector does not set to the wanted value.
func requireCSS(t *testing.T, css, selector string, want map[string]string) {
	t.Helper()
	got := cssDeclarations(css, selector)
	for prop, value := range want {
		if got[prop] != value {
			t.Errorf("style.css: %s { %s } = %q, want %q", selector, prop, got[prop], value)
		}
	}
}

// requireContains fails for every snippet missing from src.
func requireContains(t *testing.T, where, src string, snippets ...string) {
	t.Helper()
	for _, s := range snippets {
		if !strings.Contains(src, s) {
			t.Errorf("%s: expected %q", where, s)
		}
	}
}

func TestIndexHTML_HostnameInHeader(t *testing.T) {
	t.Run("AC4 hidden host name element directly after the brand", func(t *testing.T) {
		html := readAsset(t, "index.html")
		want := regexp.MustCompile(`<header>\s*<div class="header-brand">\s*` + regexp.QuoteMeta(hostnameHeaderH1))
		if !want.MatchString(html) {
			t.Errorf("index.html: expected the header's first div (class header-brand) to start with %s", hostnameHeaderH1)
		}
	})
}

func TestIndexHTML_HostnameRowFirstInSystemCard(t *testing.T) {
	t.Run("AC6 hidden Hostname row is the card's first row", func(t *testing.T) {
		html := readAsset(t, "index.html")
		want := regexp.MustCompile(`id="card-system">\s*<h2>[^\n]*</h2>\s*` + regexp.QuoteMeta(hostnameSystemRow))
		if !want.MatchString(html) {
			t.Errorf("index.html: expected %s directly after the System card's <h2>", hostnameSystemRow)
		}
	})
}

func TestAppJS_HostnameRenderedPerSnapshot(t *testing.T) {
	js := readAsset(t, "app.js")

	t.Run("AC4 header element toggled and filled", func(t *testing.T) {
		body := requireHostnameRenderer(t, js)
		requireContains(t, "renderHostname", body,
			"document.getElementById('header-hostname').classList.toggle('hidden', !name);",
			"setText('header-hostname-name', name);",
			"document.getElementById('header-hostname-name').title = name;",
		)
	})

	t.Run("AC4 rendered only from renderMetrics", func(t *testing.T) {
		if got := strings.Count(js, "renderHostname("); got != 2 {
			t.Errorf("app.js: renderHostname( occurs %d times, want 2 (its definition and one call)", got)
		}
		call := regexp.MustCompile(`// System\s*renderHostname\(snap\.system\.hostname \|\| ''\);\s*setText\('sys-kernel',`)
		if !call.MatchString(jsFunctionBody(js, "function renderMetrics(snap)")) {
			t.Error("renderMetrics: expected renderHostname(snap.system.hostname || ''); first in the // System block, before setText('sys-kernel', …)")
		}
	})

	t.Run("AC5 tab title with and without a host name", func(t *testing.T) {
		body := requireHostnameRenderer(t, js)
		requireContains(t, "renderHostname", body, "document.title = name ? name + ' – PiMonitor' : 'PiMonitor';")
		if got := strings.Count(js, "document.title"); got != 1 {
			t.Errorf("app.js: document.title occurs %d times, want 1 (only in renderHostname)", got)
		}
	})

	t.Run("AC6 row toggled and filled", func(t *testing.T) {
		body := requireHostnameRenderer(t, js)
		requireContains(t, "renderHostname", body,
			"document.getElementById('sys-hostname-row').classList.toggle('hidden', !name);",
			"setText('sys-hostname', name);",
		)
	})
}

// isAllowedHostnameWrite reports whether stmt is one of the text-only writes
// renderHostname may make.
func isAllowedHostnameWrite(stmt string) bool {
	return strings.HasPrefix(stmt, "setText(") ||
		strings.HasPrefix(stmt, "document.title =") ||
		strings.HasSuffix(stmt, ".title = name") ||
		strings.Contains(stmt, ".classList.toggle(")
}

func TestAppJS_HostnameOnlyAsText(t *testing.T) {
	js := readAsset(t, "app.js")

	t.Run("AC7 every statement is an allowed write", func(t *testing.T) {
		body := requireHostnameRenderer(t, js)
		for _, stmt := range strings.Split(body, ";") {
			stmt = strings.Join(strings.Fields(stmt), " ")
			if stmt != "" && !isAllowedHostnameWrite(stmt) {
				t.Errorf("renderHostname: statement %q is not one of setText(, document.title =, .title = name, .classList.toggle(", stmt)
			}
		}
		setText := jsFunctionBody(js, "function setText(id, text)")
		if !strings.Contains(setText, ".textContent = ") || strings.Contains(setText, "innerHTML") {
			t.Errorf("setText: expected a textContent-only write, got %q", setText)
		}
	})

	t.Run("AC7 no HTML or script sink", func(t *testing.T) {
		body := requireHostnameRenderer(t, js)
		for _, sink := range []string{
			"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write", "createContextualFragment",
			"DOMParser", "parseFromString", "setHTMLUnsafe", "srcdoc", "setAttribute(", "eval(", "Function(",
		} {
			if strings.Contains(body, sink) {
				t.Errorf("renderHostname: contains the HTML or script sink %q", sink)
			}
		}
	})
}

func TestStyleCSS_HostnameStyle(t *testing.T) {
	css := readAsset(t, "style.css")

	t.Run("AC4 muted normal-weight style", func(t *testing.T) {
		requireCSS(t, css, ".header-hostname", map[string]string{
			"font-weight": "normal",
			"color":       "var(--muted)",
		})
	})

	t.Run("AC8 name truncates with an ellipsis", func(t *testing.T) {
		requireCSS(t, css, ".header-hostname-name", map[string]string{
			"overflow":      "hidden",
			"text-overflow": "ellipsis",
			"white-space":   "nowrap",
			"min-width":     "0",
			"flex":          "0 1 auto",
		})
	})

	t.Run("AC8 header row and name wrapper are flex rows that may shrink", func(t *testing.T) {
		flexShrinkable := map[string]string{"display": "flex", "min-width": "0"}
		requireCSS(t, css, "header h1", flexShrinkable)
		requireCSS(t, css, ".header-hostname", flexShrinkable)
	})

	t.Run("AC8 brand column may shrink", func(t *testing.T) {
		requireCSS(t, css, ".header-brand", map[string]string{"min-width": "0"})
	})
}
