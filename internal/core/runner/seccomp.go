package runner

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"github.com/langgenius/dify-sandbox/internal/static"
	sg "github.com/seccomp/libseccomp-golang"
)

type SeccompRunner struct {
}

func (s *SeccompRunner) WithSeccomp(closures func() error) error {
	ctx, err := sg.NewFilter(sg.ActKillProcess)
	if err != nil {
		return err
	}
	defer ctx.Release()

	for call := range static.ALLOW_SYSCALLS {
		err = ctx.AddRule(sg.ScmpSyscall(static.ALLOW_SYSCALLS[call]), sg.ActAllow)
		if err != nil {
			return err
		}
	}

	reader, writer, err := os.Pipe()
	if err != nil {
		return err
	}
	defer reader.Close()
	defer writer.Close()

	file := os.NewFile(uintptr(writer.Fd()), "pipe")
	ctx.ExportBPF(file)

	// read from pipe
	data := make([]byte, 4096)
	n, err := reader.Read(data)
	if err != nil {
		return err
	}
	// load bpf
	sock_filters := make([]syscall.SockFilter, n/8)
	bytesBuffer := bytes.NewBuffer(data)
	err = binary.Read(bytesBuffer, binary.LittleEndian, &sock_filters)
	if err != nil {
		return err
	}

	var pipe_fds [2]int
	// create stdout pipe
	err = syscall.Pipe2(pipe_fds[0:], syscall.O_CLOEXEC)
	if err != nil {
		return err
	}
	stdout_reader, stdout_writer := pipe_fds[0], pipe_fds[1]
	// create stderr pipe
	err = syscall.Pipe2(pipe_fds[0:], syscall.O_CLOEXEC)
	if err != nil {
		return err
	}
	stderr_reader, stderr_writer := pipe_fds[0], pipe_fds[1]

	// fork subprocess
	pid, _, errno := syscall.RawSyscall(syscall.SYS_FORK, 0, 0, 0)
	if errno != 0 {
		return fmt.Errorf("fork failed: %d", errno)
	}

	defer func() {
		syscall.Close(int(stdout_reader))
		syscall.Close(int(stderr_reader))
		syscall.Close(int(stdout_writer))
		syscall.Close(int(stderr_writer))
	}()

	// child process
	if pid == 0 {
		// close read end of stdout pipe
		syscall.Close(int(stdout_reader))
		// close read end of stderr pipe
		syscall.Close(int(stderr_reader))

		defer syscall.Close(int(stdout_writer))
		defer syscall.Close(int(stderr_writer))
		defer syscall.Exit(0)

		bpf := syscall.SockFprog{
			Len:    uint16(len(sock_filters)),
			Filter: &sock_filters[0],
		}

		_, _, err2 := syscall.RawSyscall6(syscall.SYS_PRCTL, syscall.PR_SET_SECCOMP, 2, uintptr(unsafe.Pointer(&bpf)), 0, 0, 0)
		if err2 != 0 {
			response := fmt.Sprintf("prctl failed: %d\n", err2)
			_, _ = syscall.Write(int(stderr_writer), []byte(response))
			return nil
		}

		_, _, err2 = syscall.RawSyscall(syscall.SYS_SETGID, uintptr(static.SANDBOX_GROUP_ID), 0, 0)
		if err2 != 0 {
			response := fmt.Sprintf("setgid failed: %v\n", err2)
			_, _ = syscall.Write(int(stderr_writer), []byte(response))
			return nil
		}

		_, _, err2 = syscall.RawSyscall(syscall.SYS_SETUID, uintptr(static.SANDBOX_USER_UID), 0, 0)
		if err2 != 0 {
			response := fmt.Sprintf("setuid failed: %v\n", err2)
			_, _ = syscall.Write(int(stderr_writer), []byte(response))
			return nil
		}

		err := closures()
		if err != nil {
			response := fmt.Sprintf("%v\n", err)
			_, _ = syscall.Write(int(stderr_writer), []byte(response))
			return nil
		}
	} else {
		// close write end of stdout pipe
		syscall.Close(int(stdout_writer))
		// close write end of stderr pipe
		syscall.Close(int(stderr_writer))
		// wait for child process to finish
		_, _, err2 := syscall.RawSyscall(syscall.SYS_WAIT4, pid, 0, 0)
		if err2 != 0 {
			return fmt.Errorf("wait4 failed: %d", err2)
		}

		// read from stderr pipe
		data := make([]byte, 4096)
		_, err := syscall.Read(int(stderr_reader), data)
		if err != nil {
			return err
		}

		fmt.Println(string(data))
	}

	return nil
}
