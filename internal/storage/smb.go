package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path"
	"strings"
	"time"

	smb2 "github.com/hirochachacha/go-smb2"
)

// SMB is a folder on a Windows share or a Samba server, reached over SMB2/3
// with NTLM. Its paths are relative to the folder inside the share.
type SMB struct {
	conn    net.Conn
	session *smb2.Session
	share   *smb2.Share
	folder  string
}

// NewSMB connects to share on host (host or host:port) and uses folder inside
// it as the remote root. domain may be empty.
func NewSMB(host, share, folder, user, password, domain string) (*SMB, error) {
	if !strings.Contains(host, ":") {
		host += ":445"
	}
	conn, err := net.DialTimeout("tcp", host, 15*time.Second)
	if err != nil {
		return nil, err
	}
	dialer := &smb2.Dialer{Initiator: &smb2.NTLMInitiator{User: user, Password: password, Domain: domain}}
	session, err := dialer.Dial(conn)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	mounted, err := session.Mount(share)
	if err != nil {
		_ = session.Logoff()
		_ = conn.Close()
		return nil, err
	}
	return &SMB{conn: conn, session: session, share: mounted, folder: strings.Trim(folder, "/")}, nil
}

func (s *SMB) full(p string) string {
	if s.folder == "" {
		return p
	}
	return path.Join(s.folder, p)
}

// List walks the folder.
func (s *SMB) List(ctx context.Context) ([]Entry, error) {
	entries := []Entry{}
	var walk func(dir string) error
	walk = func(dir string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		infos, err := s.share.ReadDir(s.full(dir))
		if err != nil {
			if dir == "" && errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		for _, info := range infos {
			name := path.Join(dir, info.Name())
			if info.IsDir() {
				if err := walk(name); err != nil {
					return err
				}
				continue
			}
			entries = append(entries, Entry{Path: name, Size: info.Size(), ModTime: info.ModTime(), Tag: tagOfInfo("", info.ModTime(), info.Size())})
		}
		return nil
	}
	if err := walk(""); err != nil {
		return nil, err
	}
	return entries, nil
}

// Get reads a file.
func (s *SMB) Get(ctx context.Context, p string) ([]byte, error) {
	f, err := s.share.Open(s.full(p))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

// Put writes a file. SMB has no atomic replace, so the old file is removed
// after the new one is complete, under a temporary name.
func (s *SMB) Put(ctx context.Context, p string, data []byte) (Entry, error) {
	if err := s.mkdirParents(p); err != nil {
		return Entry{}, err
	}
	tmp := path.Join(path.Dir(p), "."+path.Base(p)+".tmp-put")
	f, err := s.share.Create(s.full(tmp))
	if err != nil {
		return Entry{}, err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = s.share.Remove(s.full(tmp))
		return Entry{}, err
	}
	if err := f.Close(); err != nil {
		return Entry{}, err
	}
	if err := s.replace(tmp, p); err != nil {
		return Entry{}, err
	}
	info, err := s.share.Stat(s.full(p))
	if err != nil {
		return Entry{}, err
	}
	return Entry{Path: p, Size: info.Size(), ModTime: info.ModTime(), Tag: tagOfInfo("", info.ModTime(), info.Size())}, nil
}

// Move renames a file over the destination.
func (s *SMB) Move(ctx context.Context, from, to string) error {
	if err := s.mkdirParents(to); err != nil {
		return err
	}
	err := s.replace(from, to)
	if errors.Is(err, fs.ErrNotExist) {
		return ErrNotFound
	}
	return err
}

// Close ends the session and the connection.
func (s *SMB) Close() error {
	_ = s.share.Umount()
	_ = s.session.Logoff()
	return s.conn.Close()
}

func (s *SMB) replace(from, to string) error {
	if err := s.share.Remove(s.full(to)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return s.share.Rename(s.full(from), s.full(to))
}

func (s *SMB) mkdirParents(p string) error {
	dir := path.Dir(p)
	if dir == "." || dir == "" {
		return nil
	}
	if err := s.share.MkdirAll(s.full(dir), os.ModePerm); err != nil {
		return fmt.Errorf("storage: creating %s: %w", dir, err)
	}
	return nil
}
