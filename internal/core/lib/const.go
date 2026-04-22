package lib

const (
	SUCCESS           = 0
	ERR_CHROOT        = 1
	ERR_CHDIR         = 2
	ERR_SETNONEWPRIVS = 3
	ERR_SECCOMP       = 4
	ERR_SETUID        = 5
	ERR_SETGID        = 6
	ERR_SETGROPS      = 7
	ERR_UNKNOWN       = 99
)
