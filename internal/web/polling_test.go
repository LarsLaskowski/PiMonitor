package web

import (
	"regexp"
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

// TestAppJS_AsyncCallsAreNotFloating guards issue #168 (SonarCloud
// javascript:S9383): a call to one of app.js's own async functions written
// as a bare statement drops the returned promise, so any rejection surfaces
// only as an anonymous "unhandled rejection". Every such call must be
// awaited, explicitly discarded with `void` (fire-and-forget refreshes), or
// chained to a rejection handler (`main().catch(...)`) — all of which keep
// the call from starting the statement.
func TestAppJS_AsyncCallsAreNotFloating(t *testing.T) {
	data, err := assetsFS.ReadFile("assets/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	js := string(data)

	decls := regexp.MustCompile(`async function (\w+)\(`).FindAllStringSubmatch(js, -1)
	if len(decls) == 0 {
		t.Fatal("app.js: expected at least one async function declaration")
	}
	for _, d := range decls {
		bare := regexp.MustCompile(`(?m)^[ \t]*` + d[1] + `\([^)]*\);`)
		for _, loc := range bare.FindAllStringIndex(js, -1) {
			line := strings.Count(js[:loc[0]], "\n") + 1
			t.Errorf("app.js:%d: %s() is called as a bare statement; await it, prefix it with void, or attach .catch", line, d[1])
		}
	}
}

// jsFuncBody returns the text of the function whose declaration starts with
// header, cut at the first "\n  }\n" after it (the closing brace of a
// function at the IIFE's indentation, as the in-flight guard scan does).
func jsFuncBody(t *testing.T, js, header string) string {
	t.Helper()
	start := strings.Index(js, header)
	if start == -1 {
		t.Fatalf("app.js: expected %q", header)
	}
	end := strings.Index(js[start:], "\n  }\n")
	if end == -1 {
		t.Fatalf("app.js: could not find the end of %q", header)
	}
	return js[start : start+end]
}

// firstShowBlock returns the text of the `if (!initialLoadStarted) { ... }`
// block of a visibilitychange handler body, and the text after its closing
// brace. ok is false when the block is missing or unterminated.
func firstShowBlock(body string) (block, rest string, ok bool) {
	loc := regexp.MustCompile(`if\s*\(\s*!initialLoadStarted\s*\)\s*\{`).FindStringIndex(body)
	if loc == nil {
		return "", "", false
	}
	depth := 1
	for i := loc[1]; i < len(body); i++ {
		switch body[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return body[loc[1]:i], body[i+1:], true
			}
		}
	}
	return "", "", false
}

// checkFirstShow reports why a handler body does not run the initial load
// instead of the four-poll refresh on the first show ("" when it does): the
// `if (!initialLoadStarted)` block must start initialLoad() and either end
// in `return;` or have the refresh in an else, so the first show never runs
// both.
func checkFirstShow(body string) string {
	block, rest, ok := firstShowBlock(body)
	if !ok {
		return "no `if (!initialLoadStarted) {...}` block in the visibilitychange handler"
	}
	if !regexp.MustCompile(`\binitialLoad\(\)`).MatchString(block) {
		return "the `if (!initialLoadStarted)` block does not call initialLoad()"
	}
	if strings.Contains(block, "pollMetrics()") {
		return "the `if (!initialLoadStarted)` block also runs the refresh (pollMetrics())"
	}
	endsInReturn := regexp.MustCompile(`return;\s*$`).MatchString(block)
	hasElse := regexp.MustCompile(`^\s*else\b`).MatchString(rest)
	if !endsInReturn && !hasElse {
		return "the `if (!initialLoadStarted)` block neither ends in `return;` nor has the refresh in an else: the first show would run both"
	}
	return ""
}

// TestAppJS_InitialLoadWaitsForVisibility guards issue #203: a page loaded
// in a background tab must not poll the Pi. main starts the initial load
// only while the page is visible; the first show runs it instead of the
// refresh of a later show. A source scan, since the repository has no
// JavaScript test runner.
func TestAppJS_InitialLoadWaitsForVisibility(t *testing.T) {
	data, err := assetsFS.ReadFile("assets/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	js := string(data)

	t.Run("AC1 main starts the initial load only when the page is visible", func(t *testing.T) { checkMainGuard(t, js) })
	t.Run("AC1 initialLoad marks the load started before its first request", func(t *testing.T) { checkInitialLoadFlag(t, js) })
	t.Run("AC1 the first show runs the initial load instead of the refresh", func(t *testing.T) { checkHandlerFirstShow(t, js) })
	t.Run("AC1 the first-show check fails on a handler without the return", checkFirstShowNegatives)
}

func checkMainGuard(t *testing.T, js string) {
	body := jsFuncBody(t, js, "async function main(")
	if !regexp.MustCompile(`if\s*\(\s*!document\.hidden\s*\)\s*await initialLoad\(\);`).MatchString(body) {
		t.Error("app.js: main should contain `if (!document.hidden) await initialLoad();`")
	}
	if strings.Contains(body, "reloadAll(") {
		t.Error("app.js: main must not call reloadAll() directly; the initial load goes through initialLoad()")
	}
}

func checkInitialLoadFlag(t *testing.T, js string) {
	body := jsFuncBody(t, js, "async function initialLoad(")
	flag := strings.Index(body, "initialLoadStarted = true")
	load := strings.Index(body, "await reloadAll()")
	if flag == -1 || load == -1 || flag > load {
		t.Errorf("app.js: initialLoad should set initialLoadStarted = true before `await reloadAll()` (flag at %d, load at %d)", flag, load)
	}
}

func checkHandlerFirstShow(t *testing.T, js string) {
	body := jsFuncBody(t, js, "function wireVisibilityPolling(")
	if msg := checkFirstShow(body); msg != "" {
		t.Errorf("app.js: %s", msg)
	}
	guard := strings.Index(body, "initialLoadStarted")
	poll := strings.Index(body, "pollMetrics()")
	if guard == -1 || poll == -1 || guard > poll {
		t.Errorf("app.js: the initialLoadStarted check must precede the first pollMetrics() (check at %d, poll at %d)", guard, poll)
	}
}

func checkFirstShowNegatives(t *testing.T) {
	noReturn := `document.addEventListener('visibilitychange', () => {
      if (document.hidden) { stopPolling(); return; }
      if (!initialLoadStarted) {
        initialLoad().catch((e) => console.error(e));
      }
      void pollMetrics();
      startPolling();
    });`
	if checkFirstShow(noReturn) == "" {
		t.Error("checkFirstShow accepted a handler whose first-show block has no return and no else")
	}
	withReturn := strings.Replace(noReturn, "console.error(e));", "console.error(e));\n        return;", 1)
	if msg := checkFirstShow(withReturn); msg != "" {
		t.Errorf("checkFirstShow rejected a handler with the return: %s", msg)
	}
	withElse := strings.Replace(noReturn, "      void pollMetrics();", "      else { void pollMetrics(); }\n      //", 1)
	if msg := checkFirstShow(withElse); msg != "" {
		t.Errorf("checkFirstShow rejected a handler with the refresh in an else: %s", msg)
	}
	if checkFirstShow("if (document.hidden) { return; } void pollMetrics();") == "" {
		t.Error("checkFirstShow accepted a handler without the initialLoadStarted block")
	}
}

// TestAppJS_ReloadStartsTimersOnlyWhenVisible guards issue #203: a load the
// hide interrupts (the initial load, the reload after an accepted API key)
// runs to its end but must not re-arm the timers the hide just stopped.
func TestAppJS_ReloadStartsTimersOnlyWhenVisible(t *testing.T) {
	data, err := assetsFS.ReadFile("assets/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	body := jsFuncBody(t, string(data), "async function reloadAll(")

	t.Run("AC2 reloadAll starts the timers only while visible", func(t *testing.T) {
		if !regexp.MustCompile(`if\s*\(\s*!document\.hidden\s*\)\s*startPolling\(\);`).MatchString(body) {
			t.Error("app.js: reloadAll should contain `if (!document.hidden) startPolling();`")
		}
		if regexp.MustCompile(`(?m)^[ \t]*startPolling\(\);`).MatchString(body) {
			t.Error("app.js: reloadAll must not call startPolling() unconditionally on a line of its own")
		}
	})
}
