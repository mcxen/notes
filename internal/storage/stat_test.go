package storage

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

// Stat must describe a file exactly as List does: a sync compares the two to
// tell whether a file changed since it was listed.
func statAgreesWithList(t *testing.T, r Remote) {
	t.Helper()
	ctx := context.Background()
	if _, err := r.Stat(ctx, "pages/p_1.json"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a missing file: %v", err)
	}
	put, err := r.Put(ctx, "pages/p_1.json", []byte(`{"format":1}`))
	if err != nil {
		t.Fatal(err)
	}
	listed, err := r.List(ctx)
	if err != nil || len(listed) != 1 {
		t.Fatalf("list = %v %v", listed, err)
	}
	got, err := r.Stat(ctx, "pages/p_1.json")
	if err != nil {
		t.Fatal(err)
	}
	if got.Tag != listed[0].Tag || got.Tag != put.Tag || got.Path != "pages/p_1.json" || got.Size != listed[0].Size {
		t.Fatalf("stat %+v, listed %+v, put %+v", got, listed[0], put)
	}
	if _, err := r.Stat(ctx, "pages"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a folder is not a file: %v", err)
	}
	if _, err := r.Stat(ctx, "nowhere/p_2.json"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a file in a missing folder: %v", err)
	}
	if _, err := r.Put(ctx, "pages/p_1.json", []byte(`{"format":1,"more":true}`)); err != nil {
		t.Fatal(err)
	}
	if changed, err := r.Stat(ctx, "pages/p_1.json"); err != nil || changed.Tag == got.Tag {
		t.Fatalf("a changed file keeps its tag: %+v %v", changed, err)
	}
}

func TestStatDescribesAFileAsListDoes(t *testing.T) {
	t.Run("folder", func(t *testing.T) { statAgreesWithList(t, Dir{Root: t.TempDir()}) })
	t.Run("webdav", func(t *testing.T) {
		endpoint, _ := newWebDAVServer(t, "ann", "secret")
		w, err := NewWebDAV(endpoint, "ann", "secret", nil)
		if err != nil {
			t.Fatal(err)
		}
		statAgreesWithList(t, WithRetry(w))
	})
}

// A server that takes the connection and then says nothing must not hold a
// sync for ever.
func TestSMBGivesUpOnAServerThatDoesNotAnswer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	held := make(chan net.Conn, 4)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			held <- c // kept open, and never answered
		}
	}()
	defer func() {
		for len(held) > 0 {
			(<-held).Close()
		}
	}()
	before := smbHandshakeTimeout
	smbHandshakeTimeout = 300 * time.Millisecond
	defer func() { smbHandshakeTimeout = before }()

	done := make(chan error, 1)
	go func() {
		_, err := NewSMB(ln.Addr().String(), "share", "", "ann", "secret", "")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil || ProblemOf(err) != ProblemTimeout {
			t.Fatalf("a silent server is %v (%v), want a timeout", ProblemOf(err), err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("NewSMB is still waiting for a server that does not answer")
	}
}

func TestALargeFileIsGivenTheTimeItNeeds(t *testing.T) {
	if got := transferTimeout(0); got != smbOpTimeout {
		t.Fatalf("an empty file gets %v", got)
	}
	// 64 MB at the slowest link waited on is some 34 minutes more.
	if got := transferTimeout(64 << 20); got != smbOpTimeout+2048*time.Second {
		t.Fatalf("a 64 MB file gets %v", got)
	}
}
