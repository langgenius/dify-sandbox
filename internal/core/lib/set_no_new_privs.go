package lib

import (
	"syscall"
)

func SetNoNewPrivs() error {
	_, _, e := syscall.Syscall6(syscall.SYS_PRCTL, 0x26, 1, 0, 0, 0, 0)
	if e != 0 {
		return e
	}
	return nil
}
