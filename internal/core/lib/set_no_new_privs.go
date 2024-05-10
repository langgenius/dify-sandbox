package lib

import (
	"syscall"
)

const (
	SeccompSetModeFilter   = 0x1
	SeccompFilterFlagTSYNC = 0x1
)

func SetNoNewPrivs() error {
	_, _, e := syscall.Syscall6(syscall.SYS_PRCTL, 0x26, 1, 0, 0, 0, 0)
	if e != 0 {
		return e
	}
	return nil
}
