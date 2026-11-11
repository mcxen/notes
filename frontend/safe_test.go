package frontend

import (
	"testing"
	"time"
)

func TestGoSafeCatchesAPanic(t *testing.T) {
	done := make(chan struct{})
	goSafe("test", func() {
		defer close(done)
		panic("boom")
	})
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the goroutine never ran")
	}
	// Reaching here means the test binary is still alive.
}

func TestGuardLetsALoopGoOn(t *testing.T) {
	rounds := 0
	for i := 0; i < 3; i++ {
		guard("round", func() {
			rounds++
			if i == 1 {
				panic("a bad round")
			}
		})
	}
	if rounds != 3 {
		t.Fatalf("rounds = %d, want 3", rounds)
	}
}
