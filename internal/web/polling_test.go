package web

import (
	"strings"
	"testing"
)

// TestAppJS_PollingPausesWhenHidden guards issue #111: a dashboard tab left
// open in the background (or a wall-mounted display with the screen off)
// must stop polling rather than hammering the Pi indefinitely. The fix
// wires a visibilitychange listener that stops the poll timers while the
// document is hidden and refreshes immediately on return.
func TestAppJS_PollingPausesWhenHidden(t *testing.T) {
	data, err := assetsFS.ReadFile("assets/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	js := string(data)

	if !strings.Contains(js, "visibilitychange") {
		t.Error("app.js: expected a visibilitychange listener to pause polling while the tab is hidden")
	}
	if !strings.Contains(js, "document.hidden") {
		t.Error("app.js: expected the visibilitychange handler to check document.hidden")
	}
	if !strings.Contains(js, "wireVisibilityPolling") {
		t.Error("app.js: expected a wireVisibilityPolling() call wired up from main()")
	}
}

// TestAppJS_PollFunctionsGuardInFlightRequests guards issue #111: pollMetrics
// and pollHistory must not let requests pile up when a previous request is
// still outstanding (a loaded Pi or flaky Wi-Fi can make a response take
// longer than the poll interval). Each poll function needs its own in-flight
// flag that is reset in a `finally` block — without `finally`, a single
// thrown error would latch the flag and stop polling permanently.
func TestAppJS_PollFunctionsGuardInFlightRequests(t *testing.T) {
	data, err := assetsFS.ReadFile("assets/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	js := string(data)

	for _, fn := range []string{"pollMetrics", "pollHistory"} {
		start := strings.Index(js, "async function "+fn+"(")
		if start == -1 {
			t.Errorf("app.js: expected an async function %s", fn)
			continue
		}
		end := strings.Index(js[start:], "\n  }\n")
		if end == -1 {
			t.Fatalf("app.js: could not find end of function %s", fn)
		}
		body := js[start : start+end]

		if !strings.Contains(body, "InFlight") {
			t.Errorf("app.js: %s should check/set an in-flight guard flag before awaiting its fetch", fn)
		}
		if !strings.Contains(body, "finally") {
			t.Errorf("app.js: %s should reset its in-flight guard flag in a finally block, or a thrown error would latch it and stop polling permanently", fn)
		}
	}
}
