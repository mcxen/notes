package frontend

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestUpdatePublicKeyMatchesMygoJSON(t *testing.T) {
	b, err := os.ReadFile("../mygo.json")
	if err != nil {
		t.Skip(err)
	}
	if !strings.Contains(string(b), updatePublicKey) {
		t.Fatal("updatePublicKey differs from mygo.json")
	}
}

func TestMirrorSourceFallsBackOnTamperedFile(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	good := []byte("real archive")
	sum := sha256.Sum256(good)
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, sum[:]))
	url := updateDownload + "v9.9.9/notes-9.9.9-to-x.delta"
	for _, tc := range []struct {
		name   string
		mirror []byte
		err    bool
		want   string
	}{{"ok", good, false, "mirror"}, {"tampered", []byte("evil archive"), false, "github"}, {"down", nil, true, "github"}} {
		var used string
		s := &mirrorSource{pub: pub, files: map[string]mirrorFile{url: {int64(len(good)), sig}}}
		s.get = func(_ context.Context, u string, _ time.Duration) (io.ReadCloser, error) {
			if strings.HasPrefix(u, updateMirror) {
				used = "mirror"
				if tc.err {
					return nil, errors.New("timeout")
				}
				return io.NopCloser(bytes.NewReader(tc.mirror)), nil
			}
			used = "github"
			return io.NopCloser(bytes.NewReader(good)), nil
		}
		r, err := s.Open(context.Background(), url)
		if err != nil {
			t.Fatal(tc.name, err)
		}
		b, _ := io.ReadAll(r)
		r.Close()
		if used != tc.want || !bytes.Equal(b, good) {
			t.Fatalf("%s: used %s, got %q", tc.name, used, b)
		}
	}
	if mirrored("https://evil.example/x") != "" {
		t.Fatal("foreign URL mirrored")
	}
}
