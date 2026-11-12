package web

import (
	"regexp"
	"strings"
	"testing"
)

// TestIndexHTML_PiholeSectionInUpdatesCard guards issue #180's dashboard
// surface: the Pi-hole line lives inside the Updates card and is hidden by
// default, so a host without Pi-hole (or a first paint before any data)
// shows no Pi-hole line at all.
func TestIndexHTML_PiholeSectionInUpdatesCard(t *testing.T) {
	data, err := assetsFS.ReadFile("assets/index.html")
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	src := string(data)

	start := strings.Index(src, `id="card-updates"`)
	if start < 0 {
		t.Fatal("index.html has no Updates card")
	}
	end := strings.Index(src[start:], "</section>")
	if end < 0 {
		t.Fatal("Updates card is not closed")
	}
	card := src[start : start+end]

	if !regexp.MustCompile(`class="pihole-section hidden" id="pihole-section"`).MatchString(card) {
		t.Errorf("expected a hidden-by-default #pihole-section inside the Updates card")
	}
	for _, id := range []string{"pihole-status", "pihole-versions", "pihole-checked", "pihole-stale"} {
		if !strings.Contains(card, `id="`+id+`"`) {
			t.Errorf("expected #%s inside the Updates card", id)
		}
	}
}

// TestAppJS_PiholeRenderedSeparatelyFromAptCount pins that the Pi-hole
// state is rendered from snap.pihole, gated on is_installed, and never
// added to the apt-only updates count.
func TestAppJS_PiholeRenderedSeparatelyFromAptCount(t *testing.T) {
	data, err := assetsFS.ReadFile("assets/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	src := string(data)

	if !strings.Contains(src, "renderPihole(snap.pihole,") {
		t.Errorf("expected app.js to render snap.pihole")
	}
	if !strings.Contains(src, "pihole.is_installed") {
		t.Errorf("expected app.js to gate the Pi-hole section on is_installed")
	}
	if !strings.Contains(src, "pihole.is_update_available") {
		t.Errorf("expected app.js to show is_update_available in the Pi-hole status")
	}
	if !strings.Contains(src, "setText('updates-count', String(snap.updates.count));") {
		t.Errorf("expected the Updates card's count to remain the apt-only snap.updates.count")
	}
}

// TestAppJS_PiholeShowsCheckAge pins that the card shows how long ago
// Pi-hole's own (daily) update check ran, derived from the Pi's clock
// (snap.timestamp / checked_at) rather than the browser's, so the lag
// behind a live `pihole -up` is visible.
func TestAppJS_PiholeShowsCheckAge(t *testing.T) {
	data, err := assetsFS.ReadFile("assets/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	src := string(data)

	for _, want := range []string{
		"renderPihole(snap.pihole, snap.timestamp)",
		"pihole.cache_age_seconds",
		"pihole.checked_at",
		"setText('pihole-checked',",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("expected app.js to contain %q", want)
		}
	}
	start := strings.Index(src, "function piholeCheckAge(")
	if start < 0 {
		t.Fatal("expected app.js to define piholeCheckAge")
	}
	end := strings.Index(src[start:], "\n  }\n")
	if end < 0 {
		t.Fatal("could not find the end of piholeCheckAge")
	}
	if strings.Contains(src[start:start+end], "Date.now()") {
		t.Errorf("piholeCheckAge must use the Pi's clock (snap.timestamp), not the browser's Date.now()")
	}
}
