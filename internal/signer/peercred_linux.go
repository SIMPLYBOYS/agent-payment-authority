package signer

import (
	"net"

	"golang.org/x/sys/unix"
)

// peerUID is the uid of the process at the other end of a Unix socket, as
// the kernel reports it (SO_PEERCRED).
func peerUID(c *net.UnixConn) (uint32, error) {
	raw, err := c.SyscallConn()
	if err != nil {
		return 0, err
	}
	var uid uint32
	var gerr error
	if err := raw.Control(func(fd uintptr) {
		var u *unix.Ucred
		if u, gerr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED); gerr == nil {
			uid = u.Uid
		}
	}); err != nil {
		return 0, err
	}
	return uid, gerr
}
