//go:build !windows

package update

import "syscall"

// detachedAttr lets the helper outlive the app.
func detachedAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
