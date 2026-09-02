package web

import (
	"regexp"
	"strings"
	"testing"
)

// TestIndexHTML_ModalsAreNativeDialogs guards the modal-backdrop-div to
// native <dialog> migration: every modal must be a <dialog> element (which
// get focus trapping, ::backdrop and Escape-to-dismiss from the browser for
// free), not a `role="dialog"` div inside a `.modal-backdrop` wrapper.
func TestIndexHTML_ModalsAreNativeDialogs(t *testing.T) {
	data, err := assetsFS.ReadFile("assets/index.html")
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	html := string(data)

	// Derive the modal ids from index.html itself — any id ending in
	// "-modal" (not "-modal-title" or "-modal-close", which the trailing
	// quote in the pattern excludes) — rather than a hardcoded list. A
	// `role="dialog"` div is already caught below regardless of id; what
	// this adds is catching a future modal declared as a plain
	// `<div class="modal" id="x-modal">` with no role at all, which a
	// hardcoded list wouldn't cover.
	modalIDs := regexp.MustCompile(`id="([\w-]+-modal)"`).FindAllStringSubmatch(html, -1)
	if len(modalIDs) == 0 {
		t.Fatal(`index.html: found no id="...-modal" elements to check`)
	}
	for _, m := range modalIDs {
		id := m[1]
		want := `<dialog class="modal" id="` + id + `"`
		if !strings.Contains(html, want) {
			t.Errorf("index.html: expected %q to be declared as %q", id, want)
		}
	}
	if strings.Contains(html, "modal-backdrop") {
		t.Error(`index.html: found "modal-backdrop"; native <dialog> elements need no separate backdrop wrapper div`)
	}
	if strings.Contains(html, `role="dialog"`) {
		t.Error(`index.html: found role="dialog"; a <dialog> element has an implicit dialog role and doesn't need it set explicitly`)
	}
}

// TestStyleCSS_DialogRuleScopedToOpen guards against the regression caught
// during manual browser testing of the <dialog> migration: an unscoped
// `dialog.modal { display: flex; ... }` rule overrides the user-agent
// default `dialog:not([open]) { display: none }`, leaving every dialog
// visible (in normal document flow) even when closed.
func TestStyleCSS_DialogRuleScopedToOpen(t *testing.T) {
	data, err := assetsFS.ReadFile("assets/style.css")
	if err != nil {
		t.Fatalf("read style.css: %v", err)
	}
	css := string(data)

	if !strings.Contains(css, "dialog.modal[open]") {
		t.Error(`style.css: expected a "dialog.modal[open]" rule so the box styling only applies while the dialog is open`)
	}

	unscoped := regexp.MustCompile(`dialog\.modal\s*\{`)
	if unscoped.MatchString(css) {
		t.Error(`style.css: found an unscoped "dialog.modal {" rule; it must be scoped to "dialog.modal[open]" or it overrides the UA default that hides a closed <dialog>`)
	}
}

// TestIndexHTML_ClickableCardHeadersUseButtons guards the
// `<h2 role="button" tabindex="0">` to real `<button>` migration: each
// clickable metric card's heading must wrap an actual button (which gets
// keyboard activation for free), not fake one with an ARIA role on a
// non-interactive element.
func TestIndexHTML_ClickableCardHeadersUseButtons(t *testing.T) {
	data, err := assetsFS.ReadFile("assets/index.html")
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	html := string(data)

	sectionByMetric := regexp.MustCompile(`(?s)data-metric="(\w+)".*?</section>`)
	found := map[string]bool{}
	for _, m := range sectionByMetric.FindAllStringSubmatch(html, -1) {
		metric, section := m[1], m[0]
		found[metric] = true
		if !strings.Contains(section, `<button type="button" class="card-header-btn"`) {
			t.Errorf("index.html: card %q's <h2> should wrap a <button class=\"card-header-btn\">, got:\n%s", metric, section)
		}
	}
	for _, want := range []string{"cpu", "load", "temperature", "memory"} {
		if !found[want] {
			t.Errorf("index.html: expected a data-metric=%q card, found none", want)
		}
	}

	if strings.Contains(html, `role="button"`) {
		t.Error(`index.html: found role="button"; clickable card headers should use a real <button> instead`)
	}
}

// TestAppJS_NoManualModalKeyHandling guards against the hand-rolled Tab
// trap / Escape routing (focusablesIn, trapTab, dismissModal, visibleModal,
// wireModalKeys) and the manual Enter/Space keydown handler on card
// headers coming back now that native <dialog> and <button> provide the
// same behavior for free.
func TestAppJS_NoManualModalKeyHandling(t *testing.T) {
	data, err := assetsFS.ReadFile("assets/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	js := string(data)

	for _, banned := range []string{"trapTab", "focusablesIn", "dismissModal", "visibleModal", "wireModalKeys"} {
		if strings.Contains(js, banned) {
			t.Errorf("app.js: found %q; native <dialog> handles focus trapping and Escape-to-dismiss, this hand-rolled helper shouldn't come back", banned)
		}
	}
	if strings.Contains(js, `e.key === 'Enter'`) || strings.Contains(js, `e.key === ' '`) {
		t.Error("app.js: found manual Enter/Space key handling; a real <button> gets keyboard activation for free")
	}
}
