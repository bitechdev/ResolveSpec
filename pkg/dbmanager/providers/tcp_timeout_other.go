//go:build !linux

package providers

import (
	"syscall"
	"time"
)

// setTCPUserTimeout is a no-op where TCP_USER_TIMEOUT is unavailable; TCP
// keepalive still applies.
func setTCPUserTimeout(time.Duration) func(network, address string, c syscall.RawConn) error {
	return nil
}
