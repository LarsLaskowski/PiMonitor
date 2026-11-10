package collector

import (
	"log/slog"
	"sync"
)

// failureLog collapses a collector's repeated, identical failures into a
// single WARN line. Optional, hardware-specific sources (e.g. `vcgencmd
// get_throttled`) can fail persistently for an environmental reason - a
// missing device permission, an unsupported board - and logging that at
// WARN on every fast tick floods the journal (~17,000 lines/day at the
// default 5 s interval, issue #176) without telling the operator anything
// new.
//
// observe logs at WARN on the first failure and whenever the error text
// changes, at DEBUG while the same error repeats, and at INFO once the
// source recovers. The zero value is ready to use.
type failureLog struct {
	mu      sync.Mutex
	failing bool
	lastErr string
}

// observe records the outcome of one collection attempt (err == nil means
// success) and logs it under msg according to the rules on failureLog.
func (f *failureLog) observe(log *slog.Logger, msg string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if err == nil {
		if f.failing {
			log.Info(msg+": recovered", "previous_error", f.lastErr)
			f.failing = false
			f.lastErr = ""
		}
		return
	}

	text := err.Error()
	if f.failing && text == f.lastErr {
		log.Debug(msg, "error", err, "repeated", true)
		return
	}
	f.failing = true
	f.lastErr = text
	log.Warn(msg, "error", err)
}
