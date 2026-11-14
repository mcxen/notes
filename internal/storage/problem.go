package storage

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"strings"
	"syscall"

	smb2 "github.com/hirochachacha/go-smb2"
)

// Problem is why a remote folder could not be used, as far as the error
// tells. It is for saying what went wrong in words a person can act on; the
// error itself is not changed.
type Problem int

const (
	// ProblemUnknown is an error this package cannot name.
	ProblemUnknown Problem = iota
	// ProblemAuth is a user name or password the server refused.
	ProblemAuth
	// ProblemDenied is an account that may not use the folder.
	ProblemDenied
	// ProblemNoFolder is a folder or share that is not on the server.
	ProblemNoFolder
	// ProblemNoHost is a server name that does not resolve.
	ProblemNoHost
	// ProblemRefused is a server that takes no connection at that address.
	ProblemRefused
	// ProblemTimeout is a server that did not answer in time.
	ProblemTimeout
	// ProblemBusy is a server that asked the client to slow down.
	ProblemBusy
	// ProblemCertificate is an HTTPS certificate that could not be trusted.
	ProblemCertificate
	// ProblemNotWebDAV is an address that answers, but not as a WebDAV folder.
	ProblemNotWebDAV
)

// problemNames are the problems as a log keeps them.
var problemNames = map[Problem]string{
	ProblemAuth:        "auth",
	ProblemDenied:      "denied",
	ProblemNoFolder:    "no-folder",
	ProblemNoHost:      "no-host",
	ProblemRefused:     "refused",
	ProblemTimeout:     "timeout",
	ProblemBusy:        "busy",
	ProblemCertificate: "certificate",
	ProblemNotWebDAV:   "not-webdav",
}

// String is the problem's name in a log; an unknown problem has none.
func (p Problem) String() string { return problemNames[p] }

// ParseProblem is the problem a log names.
func ParseProblem(name string) Problem {
	for p, n := range problemNames {
		if n == name {
			return p
		}
	}
	return ProblemUnknown
}

// NTSTATUS codes an SMB server answers with (MS-ERREF 2.3).
const (
	ntAccessDenied       = 0xC0000022
	ntObjectNameNotFound = 0xC0000034
	ntObjectPathNotFound = 0xC000003A
	ntWrongPassword      = 0xC000006A
	ntLogonFailure       = 0xC000006D
	ntAccountRestriction = 0xC000006E
	ntPasswordExpired    = 0xC0000071
	ntAccountDisabled    = 0xC0000072
	ntBadNetworkName     = 0xC00000CC
	ntAccountLockedOut   = 0xC0000234
)

// ProblemOf names what an error from a remote folder means.
func ProblemOf(err error) Problem {
	if err == nil {
		return ProblemUnknown
	}
	if errors.Is(err, ErrBusy) {
		return ProblemBusy
	}
	var response *smb2.ResponseError
	if errors.As(err, &response) {
		switch response.Code {
		case ntLogonFailure, ntWrongPassword, ntAccountRestriction, ntPasswordExpired, ntAccountDisabled, ntAccountLockedOut:
			return ProblemAuth
		case ntAccessDenied:
			return ProblemDenied
		case ntBadNetworkName, ntObjectNameNotFound, ntObjectPathNotFound:
			return ProblemNoFolder
		}
	}
	var dns *net.DNSError
	if errors.As(err, &dns) && dns.IsNotFound {
		return ProblemNoHost
	}
	var unknownAuthority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	var verification *tls.CertificateVerificationError
	if errors.As(err, &unknownAuthority) || errors.As(err, &hostname) || errors.As(err, &invalid) || errors.As(err, &verification) {
		return ProblemCertificate
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return ProblemRefused
	}
	var netErr net.Error
	var smbCtx *smb2.ContextError
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) || (errors.As(err, &smbCtx) && smbCtx.Timeout()) {
		return ProblemTimeout
	}
	// go-webdav's HTTP errors are of a type it keeps to itself: they are told
	// by their text, which starts with the status code, at any depth.
	for e := err; e != nil; e = errors.Unwrap(e) {
		switch {
		case isStatus(e, http.StatusUnauthorized):
			return ProblemAuth
		case isStatus(e, http.StatusForbidden):
			return ProblemDenied
		case isStatus(e, http.StatusNotFound), isStatus(e, http.StatusConflict):
			return ProblemNoFolder
		case isStatus(e, http.StatusMethodNotAllowed), isStatus(e, http.StatusNotImplemented):
			return ProblemNotWebDAV
		}
	}
	// A page that is not a WebDAV answer fails to parse as one.
	if strings.Contains(err.Error(), "webdav:") && strings.Contains(err.Error(), "XML") {
		return ProblemNotWebDAV
	}
	return ProblemUnknown
}
