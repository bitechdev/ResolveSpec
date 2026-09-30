//go:build linux

package providers

import (
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// setTCPUserTimeout returns a net.Dialer Control func setting TCP_USER_TIMEOUT.
func setTCPUserTimeout(d time.Duration) func(network, address string, c syscall.RawConn) error {
	return func(network, address string, c syscall.RawConn) error {
		var sockErr error
		err := c.Control(func(fd uintptr) {
			sockErr = unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_USER_TIMEOUT, int(d.Milliseconds()))
		})
		if err != nil {
			return err
		}
		return sockErr
	}
}
