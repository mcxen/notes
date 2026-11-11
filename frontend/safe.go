package frontend

import (
	"log"
	"runtime/debug"
)

// A panic in a goroutine of its own ends the whole program, with whatever the
// windows had not yet written. The background work (syncing, watching the
// folder, saving) is not worth that: it is caught here, logged with its stack,
// and the app goes on.

// recoverLog is for a deferred call: it stops a panic and logs it.
func recoverLog(what string) {
	if r := recover(); r != nil {
		log.Printf("notes: %s failed and was stopped: %v\n%s", what, r, debug.Stack())
	}
}

// goSafe runs fn in a goroutine of its own, catching a panic in it.
func goSafe(what string, fn func()) {
	go func() {
		defer recoverLog(what)
		fn()
	}()
}

// guard runs fn and catches a panic in it, so that a loop whose every round is
// guarded goes on after a round that failed.
func guard(what string, fn func()) {
	defer recoverLog(what)
	fn()
}
