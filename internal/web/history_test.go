package web

import (
	"strings"
	"testing"
)

// appJS returns the embedded dashboard script for the source-scanning
// guards below. A JS test framework is deliberately not part of this
// repository (see docs/TESTS.md), so scanning the committed source is the
// only way to guard client-side behavior that Go tests can't execute.
func appJS(t *testing.T) string {
	t.Helper()
	data, err := assetsFS.ReadFile("assets/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	return string(data)
}

// TestAppJS_HistoryFetchedIncrementally guards issue #112: the dashboard
// must ask for the points it doesn't have yet (?since=) instead of
// re-downloading the whole retained window once a minute, which is the
// single most expensive request the Pi serves.
func TestAppJS_HistoryFetchedIncrementally(t *testing.T) {
	js := appJS(t)

	if !strings.Contains(js, "/api/v1/metrics/history?since=") {
		t.Error("app.js: expected history polls to use the ?since= parameter instead of re-fetching the full window")
	}
	if !strings.Contains(js, "encodeURIComponent(since)") {
		t.Error("app.js: expected the since timestamp to be URL-encoded into the query string")
	}
	if !strings.Contains(js, "newestPoint ? newestPoint.t : null") {
		t.Error("app.js: expected the next ?since= to be the server's own timestamp string, passed back verbatim: re-formatting it through Date truncates the sub-millisecond precision and the boundary point comes back on every poll")
	}
}

// TestAppJS_HistoryRecoversFromGaps guards the client-side half of issue
// #112 that is easiest to get wrong: a delta that doesn't line up with the
// locally held window (points evicted while the tab was hidden, a restarted
// server serving restored history, a failed request) must discard the local
// state and re-fetch the full window, not append blindly.
func TestAppJS_HistoryRecoversFromGaps(t *testing.T) {
	js := appJS(t)

	start := strings.Index(js, "function mergeHistory(")
	if start == -1 {
		t.Fatal("app.js: expected a mergeHistory() that merges a ?since= delta into the held window")
	}
	merge := js[start:]
	if end := strings.Index(merge, "\n  }\n"); end != -1 {
		merge = merge[:end]
	}
	for _, want := range []struct{ snippet, why string }{
		{"incoming.oldest <= held.newest", "reject a delta whose points aren't strictly newer than what is already held"},
		{"historyGapToleranceMs()", "reject a delta that starts after a gap, i.e. points were evicted before this client saw them"},
		{"invalid", "reject a delta (or a held window) with an unparseable timestamp"},
	} {
		if !strings.Contains(merge, want.snippet) {
			t.Errorf("app.js: mergeHistory should %s (missing %q)", want.why, want.snippet)
		}
	}

	poll := js[strings.Index(js, "async function pollHistory("):]
	if end := strings.Index(poll, "\n  }\n"); end != -1 {
		poll = poll[:end]
	}
	if !strings.Contains(poll, "historyPath(null)") {
		t.Error("app.js: pollHistory should fall back to a full-window fetch when the delta cannot be merged")
	}
	if !strings.Contains(poll, "historySince = null") {
		t.Error("app.js: pollHistory should reset the since marker on a failed request, so the next poll refetches the full window")
	}
	if !strings.Contains(js, "HISTORY_RESYNC_EVERY_POLLS") {
		t.Error("app.js: expected a periodic full-window re-sync, the only way to notice a history that was replaced rather than appended to")
	}
}

// TestAppJS_HistoryTrimmedToServerWindow guards the other half of holding
// history client-side: an accumulating local window must be trimmed to
// history_window_minutes, or a dashboard left open all day would keep every
// point it ever received and chart more than the server retains.
func TestAppJS_HistoryTrimmedToServerWindow(t *testing.T) {
	js := appJS(t)

	if !strings.Contains(js, "config.history_window_minutes") {
		t.Error("app.js: expected the local window to be bounded by the server's history_window_minutes (GET /api/v1/config)")
	}
	start := strings.Index(js, "function trimHistoryWindow(")
	if start == -1 {
		t.Fatal("app.js: expected a trimHistoryWindow() bounding the locally accumulated history")
	}
	if !strings.Contains(js, "return trimHistoryWindow(merged)") {
		t.Error("app.js: expected mergeHistory to trim the merged window, or it would grow without bound")
	}
}
