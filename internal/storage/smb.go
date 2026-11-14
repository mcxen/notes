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

// How long an SMB server is waited for. A server that takes the connection
// and then says nothing would otherwise hold a sync for ever.
var (
	// smbHandshakeTimeout covers signing in and mounting the share.
	smbHandshakeTimeout = 30 * time.Second
	// smbOpTimeout covers one request: listing a folder, renaming a file.
	smbOpTimeout = 2 * time.Minute
	// smbSlowestTransfer is the slowest link a file's bytes are waited on, in
	// bytes a second: a file gets smbOpTimeout and its size at this speed.
	smbSlowestTransfer int64 = 32 << 10
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
	ctx, cancel := context.WithTimeout(context.Background(), smbHandshakeTimeout)
	defer cancel()
	dialer := &smb2.Dialer{Initiator: &smb2.NTLMInitiator{User: user, Password: password, Domain: domain}}
	session, err := dialer.DialContext(ctx, conn)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	mounted, err := session.WithContext(ctx).Mount(share)
	if err != nil {
		// The connection is closed without a goodbye: a server that stopped
		// answering would keep a logoff waiting too.
		_ = conn.Close()
		return nil, err
	}
	return &SMB{conn: conn, session: session, share: mounted, folder: strings.Trim(folder, "/")}, nil
}

// within is the share for one request that must end within d, or when ctx does.
func (s *SMB) within(ctx context.Context, d time.Duration) (*smb2.Share, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(ctx, d)
	return s.share.WithContext(ctx), cancel
}

// transferTimeout is how long a file of size bytes may take to cross.
func transferTimeout(size int64) time.Duration {
	return smbOpTimeout + time.Duration(size/smbSlowestTransfer)*time.Second
}

func (s *SMB) full(p string) string {
	if s.folder == "" {
		return p
	}
	return path.Join(s.folder, p)
}

func smbEntry(p string, info os.FileInfo) Entry {
	return Entry{Path: p, Size: info.Size(), ModTime: info.ModTime(), Tag: tagOfInfo("", info.ModTime(), info.Size())}
}

// List walks the folder.
func (s *SMB) List(ctx context.Context) ([]Entry, error) {
	entries := []Entry{}
	var walk func(dir string) error
	walk = func(dir string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		share, cancel := s.within(ctx, smbOpTimeout)
		infos, err := share.ReadDir(s.full(dir))
		cancel()
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
			entries = append(entries, smbEntry(name, info))
		}
		return nil
	}
	if err := walk(""); err != nil {
		return nil, err
	}
	return entries, nil
}

// Stat describes one file.
func (s *SMB) Stat(ctx context.Context, p string) (Entry, error) {
	share, cancel := s.within(ctx, smbOpTimeout)
	defer cancel()
	info, err := share.Stat(s.full(p))
	if errors.Is(err, fs.ErrNotExist) || (err == nil && info.IsDir()) {
		return Entry{}, ErrNotFound
	}
	if err != nil {
		return Entry{}, err
	}
	return smbEntry(p, info), nil
}

// Get reads a file. It is given the time its size needs.
func (s *SMB) Get(ctx context.Context, p string) ([]byte, error) {
	entry, err := s.Stat(ctx, p)
	if err != nil {
		return nil, err
	}
	share, cancel := s.within(ctx, transferTimeout(entry.Size))
	defer cancel()
	f, err := share.Open(s.full(p))
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
	share, cancel := s.within(ctx, transferTimeout(int64(len(data))))
	defer cancel()
	if err := mkdirParents(share, s.full(p)); err != nil {
		return Entry{}, err
	}
	tmp := path.Join(path.Dir(p), "."+path.Base(p)+".tmp-put")
	f, err := share.Create(s.full(tmp))
	if err != nil {
		return Entry{}, err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = share.Remove(s.full(tmp))
		return Entry{}, err
	}
	if err := f.Close(); err != nil {
		return Entry{}, err
	}
	if err := replace(share, s.full(tmp), s.full(p)); err != nil {
		return Entry{}, err
	}
	info, err := share.Stat(s.full(p))
	if err != nil {
		return Entry{}, err
	}
	return smbEntry(p, info), nil
}

// Move renames a file over the destination.
func (s *SMB) Move(ctx context.Context, from, to string) error {
	share, cancel := s.within(ctx, smbOpTimeout)
	defer cancel()
	if err := mkdirParents(share, s.full(to)); err != nil {
		return err
	}
	err := replace(share, s.full(from), s.full(to))
	if errors.Is(err, fs.ErrNotExist) {
		return ErrNotFound
	}
	return err
}

// Close ends the session and the connection. A server that no longer answers
// is not waited for.
func (s *SMB) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.share.WithContext(ctx).Umount()
	_ = s.session.WithContext(ctx).Logoff()
	return s.conn.Close()
}

// replace renames a file of the share over another, by its full paths.
func replace(share *smb2.Share, from, to string) error {
	if err := share.Remove(to); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return share.Rename(from, to)
}

// mkdirParents makes the folders above a file of the share, by its full path.
func mkdirParents(share *smb2.Share, full string) error {
	dir := path.Dir(full)
	if dir == "." || dir == "" {
		return nil
	}
	if err := share.MkdirAll(dir, os.ModePerm); err != nil {
		return fmt.Errorf("storage: creating %s: %w", dir, err)
	}
	return nil
}
