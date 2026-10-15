//go:build linux

package python

import (
	sg "github.com/seccomp/libseccomp-golang"
	"testing"
)

func TestIsolatedFilesystemAdditionsDoNotGrantNetworkOrChdir(t *testing.T) {
	allowed, err := isolatedFilesystemSyscalls()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"socket", "connect", "bind", "listen", "accept", "sendto", "recvfrom", "chdir", "chroot", "mount"} {
		number, err := sg.GetSyscallFromName(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range allowed {
			if int(number) == value {
				t.Fatalf("isolated file profile broadens %s", name)
			}
		}
	}
	for _, name := range []string{"getcwd", "newfstatat", "readlinkat", "getdents64"} {
		number, _ := sg.GetSyscallFromName(name)
		found := false
		for _, value := range allowed {
			found = found || value == int(number)
		}
		if !found {
			t.Fatalf("missing filesystem inspection syscall %s", name)
		}
	}
}
