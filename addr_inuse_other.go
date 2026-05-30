//go:build !windows

package main

import (
	"errors"
	"net"
	"os"
	"syscall"
)

func isAddrInUse(err error) bool {
	var opErr *net.OpError
	if !errors.As(err, &opErr) {
		return false
	}
	var sysErr *os.SyscallError
	if !errors.As(opErr.Err, &sysErr) {
		return false
	}
	var errno syscall.Errno
	return errors.As(sysErr.Err, &errno) && errno == syscall.EADDRINUSE
}
