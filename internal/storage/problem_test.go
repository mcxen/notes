package storage

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	smb2 "github.com/hirochachacha/go-smb2"
)

func TestProblemOfNamesWhatAWebDAVServerAnswered(t *testing.T) {
	endpoint, _ := newWebDAVServer(t, "ann", "secret")
	ctx := context.Background()

	wrong, _ := NewWebDAV(endpoint, "ann", "wrong", nil)
	_, err := wrong.List(ctx)
	if got := ProblemOf(err); got != ProblemAuth {
		t.Errorf("a wrong password is %v (%v), want auth", got, err)
	}
	// The sync wraps the error it reports: the problem is still told.
	if got := ProblemOf(fmt.Errorf("sync: listing the remote folder: %w", err)); got != ProblemAuth {
		t.Errorf("a wrapped wrong password is %v", got)
	}

	missing, _ := NewWebDAV(endpoint+"nowhere/", "ann", "secret", nil)
	_, err = missing.List(ctx)
	if got := ProblemOf(err); got != ProblemNoFolder {
		t.Errorf("a missing folder is %v (%v), want no-folder", got, err)
	}

	right, _ := NewWebDAV(endpoint, "ann", "secret", nil)
	if _, err := right.List(ctx); err != nil || ProblemOf(err) != ProblemUnknown {
		t.Errorf("a folder that answers has a problem: %v", err)
	}
}

func TestProblemOfNamesAServerThatCannotBeReached(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	if _, err := NewSMB(addr, "share", "", "ann", "secret", ""); ProblemOf(err) != ProblemRefused {
		t.Errorf("a closed port is %v (%v), want refused", ProblemOf(err), err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()
	if got := ProblemOf(fmt.Errorf("listing: %w", ctx.Err())); got != ProblemTimeout {
		t.Errorf("a deadline is %v, want timeout", got)
	}
	if got := ProblemOf(busy(fmt.Errorf("503 Service Unavailable"))); got != ProblemBusy {
		t.Errorf("a busy server is %v, want busy", got)
	}
}

func TestProblemOfNamesWhatAnSMBServerAnswered(t *testing.T) {
	for code, want := range map[uint32]Problem{
		0xC000006D: ProblemAuth,     // STATUS_LOGON_FAILURE
		0xC0000022: ProblemDenied,   // STATUS_ACCESS_DENIED
		0xC00000CC: ProblemNoFolder, // STATUS_BAD_NETWORK_NAME
	} {
		err := fmt.Errorf("mount: %w", &smb2.ResponseError{Code: code})
		if got := ProblemOf(err); got != want {
			t.Errorf("NTSTATUS %#x is %v, want %v", code, got, want)
		}
	}
}

func TestProblemsKeepTheirNamesInTheLog(t *testing.T) {
	for p := range problemNames {
		if ParseProblem(p.String()) != p {
			t.Errorf("%v does not come back from its name %q", p, p.String())
		}
	}
	if ParseProblem("") != ProblemUnknown || ProblemUnknown.String() != "" {
		t.Error("an unknown problem should have no name")
	}
}
