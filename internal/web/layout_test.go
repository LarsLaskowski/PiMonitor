package web

import (
	"regexp"
	"strings"
	"testing"
)

// TestIndexHTML_HasLayoutToggleAndModal guards issue #10: a header button
// must open a customization modal containing the card list the user
// toggles/reorders.
func TestIndexHTML_HasLayoutToggleAndModal(t *testing.T) {
	data, err := assetsFS.ReadFile("assets/index.html")
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	html := string(data)

	if !strings.Contains(html, `id="layout-toggle"`) {
		t.Error(`index.html: expected a button with id="layout-toggle" to open the layout customization modal`)
	}
	if !strings.Contains(html, `<dialog class="modal" id="layout-modal"`) {
		t.Error(`index.html: expected a native <dialog class="modal" id="layout-modal"> element`)
	}
	if !strings.Contains(html, `id="layout-list"`) {
		t.Error(`index.html: expected an id="layout-list" element inside the layout modal for the card list`)
	}
	if !strings.Contains(html, `id="layout-reset"`) {
		t.Error(`index.html: expected an id="layout-reset" button to restore the default layout`)
	}
}

// TestIndexHTML_MainHasNoColumnWrappers guards the flat single-list layout
// (issue #10): cards must be direct children of <main> so app.js can
// reorder/hide them by manipulating plain siblings, rather than nested
// inside fixed three-column wrapper <div>s.
func TestIndexHTML_MainHasNoColumnWrappers(t *testing.T) {
	data, err := assetsFS.ReadFile("assets/index.html")
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	html := string(data)

	if strings.Contains(html, `class="column"`) {
		t.Error(`index.html: found class="column"; cards must be flat children of <main> so they can be reordered/hidden individually (issue #10)`)
	}
}

// cardDomIDs are the ids of every card app.js's layout customization must
// know how to show/hide/reorder.
var cardDomIDs = []string{
	"card-system", "card-updates", "card-uptime", "card-cpu", "card-load",
	"card-temperature", "card-memory", "card-disks", "card-network",
}

// TestAppJS_CardDefsCoverAllCards guards issue #10: every card in
// index.html must have a corresponding entry in app.js's CARD_DEFS
// registry, or it could never be hidden/reordered through the layout
// modal, and DEFAULT_CARD_ORDER (used to reset and to backfill a stale
// stored layout) must be derived from that same registry rather than
// duplicated.
func TestAppJS_CardDefsCoverAllCards(t *testing.T) {
	data, err := assetsFS.ReadFile("assets/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	js := string(data)

	start := strings.Index(js, "const CARD_DEFS = [")
	if start == -1 {
		t.Fatal("app.js: expected a const CARD_DEFS registry")
	}
	end := strings.Index(js[start:], "];")
	if end == -1 {
		t.Fatal("app.js: could not find end of CARD_DEFS")
	}
	body := js[start : start+end]

	for _, domID := range cardDomIDs {
		if !strings.Contains(body, `domId: '`+domID+`'`) {
			t.Errorf("app.js: expected CARD_DEFS to include domId: %q", domID)
		}
	}
	if !strings.Contains(js, "const DEFAULT_CARD_ORDER = CARD_DEFS.map(") {
		t.Error("app.js: expected DEFAULT_CARD_ORDER to be derived from CARD_DEFS, not duplicated")
	}
}

// TestAppJS_LayoutPersistedToLocalStorage guards the issue's core
// acceptance criterion: hidden/reordered cards must persist across reloads,
// via localStorage (no server-side state).
func TestAppJS_LayoutPersistedToLocalStorage(t *testing.T) {
	data, err := assetsFS.ReadFile("assets/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	js := string(data)

	if !strings.Contains(js, "const LAYOUT_KEY = 'pimonitor-layout';") {
		t.Error("app.js: expected a LAYOUT_KEY localStorage key for the stored layout")
	}
	if !strings.Contains(js, "localStorage.getItem(LAYOUT_KEY)") {
		t.Error("app.js: expected storedLayout to read LAYOUT_KEY from localStorage")
	}
	if !strings.Contains(js, "localStorage.setItem(LAYOUT_KEY,") {
		t.Error("app.js: expected persistLayout to write LAYOUT_KEY to localStorage")
	}
	for _, fn := range []string{"setCardVisible(", "moveCard(", "resetLayout("} {
		if !strings.Contains(js, "function "+strings.TrimSuffix(fn, "(")+"(") {
			t.Errorf("app.js: expected a function %s", fn)
		}
		if !strings.Contains(js, "persistLayout(layout)") {
			t.Error("app.js: expected layout mutations to call persistLayout(layout) so changes survive a reload")
			break
		}
	}
}

// TestAppJS_NormalizeLayoutReconcilesStoredLayout guards against a stale or
// corrupt localStorage value silently hiding or losing a card: an unknown
// stored id must be dropped, and a known card missing from the stored
// layout must be backfilled as visible.
func TestAppJS_NormalizeLayoutReconcilesStoredLayout(t *testing.T) {
	data, err := assetsFS.ReadFile("assets/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	js := string(data)

	start := strings.Index(js, "function normalizeLayout(")
	if start == -1 {
		t.Fatal("app.js: expected a function normalizeLayout")
	}
	end := strings.Index(js[start:], "\n  }\n")
	if end == -1 {
		t.Fatal("app.js: could not find end of function normalizeLayout")
	}
	body := js[start : start+end]

	if !strings.Contains(body, "known.has(entry.id)") {
		t.Error("app.js: expected normalizeLayout to drop stored entries whose id is not a known card")
	}
	if !strings.Contains(body, "if (!seen.has(id))") {
		t.Error("app.js: expected normalizeLayout to backfill any known card missing from the stored layout")
	}
	if !strings.Contains(js, "let layout = normalizeLayout(storedLayout());") {
		t.Error("app.js: expected the initial layout state to go through normalizeLayout")
	}
}

// TestAppJS_ApplyLayoutReordersAndHidesCards guards the reorder/hide
// mechanism itself: applyLayout must move each card to its stored position
// via appendChild and toggle its "hidden" class from the stored visibility
// — except "network", whose visibility is decided in applyNetworkVisibility
// (see TestAppJS_NetworkVisibilityRespectsLayoutAndCapability).
func TestAppJS_ApplyLayoutReordersAndHidesCards(t *testing.T) {
	data, err := assetsFS.ReadFile("assets/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	js := string(data)

	start := strings.Index(js, "function applyLayout(")
	if start == -1 {
		t.Fatal("app.js: expected a function applyLayout")
	}
	end := strings.Index(js[start:], "\n  }\n")
	if end == -1 {
		t.Fatal("app.js: could not find end of function applyLayout")
	}
	body := js[start : start+end]

	if !strings.Contains(body, "main.appendChild(el)") {
		t.Error("app.js: expected applyLayout to reorder cards via main.appendChild(el)")
	}
	if !strings.Contains(body, "def.id !== 'network'") {
		t.Error("app.js: expected applyLayout to skip toggling the 'hidden' class for the network card, which applyNetworkVisibility decides")
	}
	if !strings.Contains(body, "el.classList.toggle('hidden', !entry.visible)") {
		t.Error("app.js: expected applyLayout to toggle each card's 'hidden' class from its stored visibility")
	}

	for _, wireSite := range []string{"main"} {
		fnStart := strings.Index(js, "async function "+wireSite+"(")
		if fnStart == -1 {
			t.Fatalf("app.js: expected an async function %s", wireSite)
		}
		fnBody := js[fnStart:]
		body := fnBody[:strings.Index(fnBody, "\n  }\n")]
		if !strings.Contains(body, "applyLayout()") {
			t.Errorf("app.js: expected %s to call applyLayout() so the stored layout applies on load", wireSite)
		}
		if !strings.Contains(body, "wireLayoutModal()") {
			t.Errorf("app.js: expected %s to call wireLayoutModal() so the layout modal's gear button is wired up", wireSite)
		}
	}
}

// TestAppJS_ToggleAndResetRerenderNetworkImmediately guards a follow-up from
// the #152 review (issue #153): applyLayout never toggles the network
// card's "hidden" class (see TestAppJS_ApplyLayoutReordersAndHidesCards),
// so setCardVisible and resetLayout must each re-run applyNetworkVisibility
// on the held snapshot, or unchecking "Network" in the layout modal would
// have no visible effect until the next poll. This calls the narrower
// applyNetworkVisibility rather than the full renderMetrics so a layout
// change doesn't also re-stamp "Last updated" and mask a stale
// "Connection error" state after a failed poll.
func TestAppJS_ToggleAndResetRerenderNetworkImmediately(t *testing.T) {
	data, err := assetsFS.ReadFile("assets/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	js := string(data)

	for _, fn := range []string{"setCardVisible", "resetLayout"} {
		var start int
		switch fn {
		case "setCardVisible":
			start = strings.Index(js, "function setCardVisible(")
		case "resetLayout":
			start = strings.Index(js, "function resetLayout(")
		}
		if start == -1 {
			t.Fatalf("app.js: expected a function %s", fn)
		}
		end := strings.Index(js[start:], "\n  }\n")
		if end == -1 {
			t.Fatalf("app.js: could not find end of function %s", fn)
		}
		body := js[start : start+end]
		if !strings.Contains(body, "if (latestSnapshot) applyNetworkVisibility(latestSnapshot);") {
			t.Errorf("app.js: expected %s to call applyNetworkVisibility(latestSnapshot) when a snapshot is held, so the network card's visibility updates immediately", fn)
		}
	}
}

// TestAppJS_MoveCardFocusFallsBackToResetButton guards another #153
// follow-up: moveCard's keyboard-focus restore must not land on a disabled
// checkbox. That happens when the moved card is "network" with the
// network_enabled capability off (its checkbox is disabled) and the move
// landed it at a list end (its remaining move button is also disabled) —
// focus must fall back to the always-focusable "Reset to default" button.
func TestAppJS_MoveCardFocusFallsBackToResetButton(t *testing.T) {
	data, err := assetsFS.ReadFile("assets/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	js := string(data)

	start := strings.Index(js, "function moveCard(")
	if start == -1 {
		t.Fatal("app.js: expected a function moveCard")
	}
	end := strings.Index(js[start:], "\n  }\n")
	if end == -1 {
		t.Fatal("app.js: could not find end of function moveCard")
	}
	body := js[start : start+end]

	if !strings.Contains(body, "checkbox && !checkbox.disabled && checkbox") {
		t.Error("app.js: expected moveCard's focus target to skip the checkbox when it is disabled")
	}
	if !strings.Contains(body, "document.getElementById('layout-reset')") {
		t.Error("app.js: expected moveCard's focus target to fall back to the #layout-reset button when neither a move button nor the checkbox is focusable")
	}
}

// TestAppJS_NetworkVisibilityRespectsLayoutAndCapability guards the issue's
// second acceptance criterion: a metric disabled on the server
// (network_enabled: false) must never appear, regardless of the stored
// layout's visibility preference for it. Also guards that renderMetrics
// delegates to applyNetworkVisibility rather than duplicating the check.
func TestAppJS_NetworkVisibilityRespectsLayoutAndCapability(t *testing.T) {
	data, err := assetsFS.ReadFile("assets/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	js := string(data)

	start := strings.Index(js, "function applyNetworkVisibility(")
	if start == -1 {
		t.Fatal("app.js: expected a function applyNetworkVisibility")
	}
	end := strings.Index(js[start:], "\n  }\n")
	if end == -1 {
		t.Fatal("app.js: could not find end of function applyNetworkVisibility")
	}
	block := js[start : start+end]

	if !strings.Contains(block, "layout.find(e => e.id === 'network')") {
		t.Error("app.js: expected the network visibility check to read the stored layout preference for 'network'")
	}
	if !strings.Contains(block, "networkPref && config.network_enabled && snap.network?.length") {
		t.Error("app.js: expected the network card to require the layout preference AND the server capability flag AND data before showing")
	}

	renderStart := strings.Index(js, "function renderMetrics(")
	if renderStart == -1 {
		t.Fatal("app.js: expected a function renderMetrics")
	}
	renderEnd := strings.Index(js[renderStart:], "\n  }\n")
	if renderEnd == -1 {
		t.Fatal("app.js: could not find end of function renderMetrics")
	}
	if !strings.Contains(js[renderStart:renderStart+renderEnd], "applyNetworkVisibility(snap)") {
		t.Error("app.js: expected renderMetrics to delegate to applyNetworkVisibility(snap) instead of duplicating the network visibility check")
	}
}

// TestAppJS_LayoutModalWiredAndAccessible guards the modal's wiring
// (matching the other <dialog>-based modals: close button, backdrop
// dismiss, focus return) and that each move button has a descriptive
// aria-label rather than relying on its arrow glyph alone.
func TestAppJS_LayoutModalWiredAndAccessible(t *testing.T) {
	data, err := assetsFS.ReadFile("assets/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	js := string(data)

	start := strings.Index(js, "function wireLayoutModal(")
	if start == -1 {
		t.Fatal("app.js: expected a function wireLayoutModal")
	}
	end := strings.Index(js[start:], "\n  }\n")
	if end == -1 {
		t.Fatal("app.js: could not find end of function wireLayoutModal")
	}
	body := js[start : start+end]

	if !strings.Contains(body, "wireBackdropDismiss(dialog)") {
		t.Error("app.js: expected wireLayoutModal to wire backdrop dismiss like the other modals")
	}
	if !strings.Contains(body, "wireModalFocusReturn(dialog)") {
		t.Error("app.js: expected wireLayoutModal to wire focus return like the other modals")
	}
	if !strings.Contains(body, "resetLayout") {
		t.Error("app.js: expected the reset button to call resetLayout")
	}

	if !strings.Contains(js, `upBtn.setAttribute('aria-label', 'Move ' + def.label + ' up')`) ||
		!strings.Contains(js, `downBtn.setAttribute('aria-label', 'Move ' + def.label + ' down')`) {
		t.Error("app.js: expected each move button to get a descriptive aria-label naming the card and direction")
	}
}

// TestStyleCSS_MainUsesFlatMultiColumnLayout guards the CSS side of the
// flat-list refactor: cards are laid out as plain siblings distributed by
// CSS multi-column (so app.js can reorder them by DOM order alone), with
// break-inside: avoid so a card is never split across two columns, matching
// the previous three-column breakpoints (3/2/1).
func TestStyleCSS_MainUsesFlatMultiColumnLayout(t *testing.T) {
	data, err := assetsFS.ReadFile("assets/style.css")
	if err != nil {
		t.Fatalf("read style.css: %v", err)
	}
	css := string(data)

	mainRule := regexp.MustCompile(`(?s)\nmain\s*\{[^}]*column-count:\s*3;[^}]*\}`)
	if !mainRule.MatchString(css) {
		t.Error(`style.css: expected "main { column-count: 3; ... }"`)
	}
	if !strings.Contains(css, "break-inside: avoid;") {
		t.Error(`style.css: expected ".card" to set "break-inside: avoid;" so a card is never split across columns`)
	}
	if strings.Contains(css, "grid-template-columns") {
		t.Error(`style.css: found "grid-template-columns"; main should use CSS multi-column (column-count) instead of an explicit column grid, now that cards are flat siblings`)
	}
	for _, width := range []string{"1100px", "720px"} {
		if !strings.Contains(css, "@media (max-width: "+width+") {\n  main {\n    column-count:") {
			t.Errorf("style.css: expected the %s breakpoint to adjust main's column-count", width)
		}
	}
}
