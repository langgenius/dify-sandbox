## FAQ

### 1. Why does my Python code throw an exception like "xxx.so: cannot open shared object file: No such file or directory"?

This occurs because the `dify-sandbox` implementation generates a temporary file in the `/var/sandbox/sandbox-python/` directory to save and execute your Python code. Before running your Python code, it uses `syscall.Chroot` to restrict the current process's root to the `/var/sandbox/sandbox-python/` directory. This directory structure, visible to the Python process, determines all the Python modules/packages that can be imported, including modules based on C code.

- Root: `/var/sandbox/sandbox-python/` is the root directory from the Python process perspective. Its subdirectories depend on the `python_lib_path` configuration in your `config.yaml`. Usually, it includes:
  - `etc/` directory
  - `python.so` shared object, compiled and built by `dify-sandbox`
  - `usr/lib` directory
  - `usr/local/`

If you haven't configured `python_lib_path`, `dify-sandbox` will default to the following settings (see code [internal/static/config_default_amd64.go](https://github.com/langgenius/dify-sandbox/blob/main/internal/static/config_default_amd64.go); for ARM systems, see `config_default_arm64.go`):

```go
var DEFAULT_PYTHON_LIB_REQUIREMENTS = []string{
    "/usr/local/lib/python3.10", // Usually your Python installation directory; if using conda, modify this to the conda virtual environment root directory, e.g., /root/anaconda3/envs/{env_name}
    "/usr/lib/python3.10",
    "/usr/lib/python3",
    "/usr/lib/x86_64-linux-gnu/libssl.so.3", // Your Python code's shared object dependency; it will be copied to /var/sandbox/sandbox-python/usr/lib/x86_64-linux-gnu/, and your Python process will load it from /usr/lib/x86_64-linux-gnu/
    "/usr/lib/x86_64-linux-gnu/libcrypto.so.3", // Similar to above
    "/etc/ssl/certs/ca-certificates.crt",
    "/etc/nsswitch.conf",
    "/etc/hosts",
    "/etc/resolv.conf",
    "/run/systemd/resolve/stub-resolv.conf",
    "/run/resolvconf/resolv.conf",
}
```

So, when encountering such errors, you need to modify the `python_lib_path` in your `config.yaml` to include the shared object paths required by your Python code. For example:
```config.yaml
python_path: /usr/local/bin/python3
python_lib_path:
  - /usr/local/lib/python3.10
  - /usr/lib/python3.10
  - /usr/lib/python3
  - /usr/lib/x86_64-linux-gnu/libssl.so.3
  - /usr/lib/x86_64-linux-gnu/libcrypto.so.3
  - /etc/ssl/certs/ca-certificates.crt
  - /etc/nsswitch.conf
  - /etc/hosts
  - /etc/resolv.conf
  - /run/systemd/resolve/stub-resolv.conf
  - /run/resolvconf/resolv.conf
  - *** add path which you required here ***
```

**Note:** The Go process initializes this environment at startup, so if you configure too many `python_lib_path`, the startup will be very slow. For serverless environments, consider modifying the code to complete this build in a Docker container.

### 2. My Python code returns an "operation not permitted" error?

`dify-sandbox` uses Linux seccomp to restrict system calls. It’s recommended to read the source code ([internal/core/lib/python/add_seccomp.go](https://github.com/langgenius/dify-sandbox/blob/main/internal/core/lib/python/add_seccomp.go)). When you encounter this error, it usually means your code executed a restricted system call. The default allowed system calls are configured in [syscalls_amd64](https://github.com/langgenius/dify-sandbox/blob/main/internal/static/python_syscall/syscalls_amd64.go). You can modify this according to your system’s needs (currently, it cannot be modified through the configuration file).

To quickly identify the system calls your Python code depends on, here is the recommended method:

1. Modify the `/cmd/test/syscall_dig/test.py`, add your own code besides or in the `main` function. For example, you can add `import numpy` before `main`.

2. Run `go run cmd/test/syscall_dig/main.go`, the output will like this:
```shell
~/dify-sandbox$ go run cmd/test/syscall_dig/main.go
failed with signal: bad system call
...
failed with signal: bad system call
Following syscalls are required: 0,1,3,5,8,9,10,11,12,13,14,15,16,17,24,28,35,39,60,63,105,106,131,186,202,204,217,231,233,234,237,257,262,273,281,291,318,334,435
```
If you haven't got output like this format, maybe it's your permission problem, try run it with `sudo` again.

3. These syscalls is the sandbox already added: `0,1,3,8,9,10,11,12,13,14,15,16,16,24,25,35,39,60,96,102,105,106,110,131,186,201,202,217,228,230,231,233,234,257,262,270,273,291,318,334`. You need to compare what is the extras syscall numbers of previous step. You can use a simple script or ask LLM to archive that. In this case, it's `5, 17, 28, 63, 204, 237, 281, 435`

4. add the correct syscall alias in [/internal/static/python_syscall/syscalls_amd64.go](./internal/static/python_syscall/syscalls_amd64.go), you can find it in the golang lib, like`/usr/lib/go-1.18/src/syscall/zsysnum_linux_amd64.go`
```golang
var ALLOW_SYSCALLS = []int{
	// file io
	syscall.SYS_NEWFSTATAT, syscall.SYS_IOCTL, syscall.SYS_LSEEK, syscall.SYS_GETDENTS64,
	syscall.SYS_WRITE, syscall.SYS_CLOSE, syscall.SYS_OPENAT, syscall.SYS_READ,
    
	...

	// run numpy required
	syscall.SYS_FSTAT, syscall.SYS_PREAD64, syscall.SYS_MADVISE, syscall.SYS_UNAME,
	syscall.SYS_SCHED_GETAFFINITY, syscall.SYS_MBIND, syscall.SYS_EPOLL_PWAIT, 435,
}
```
If the syscall alias not defined in golang, you can direcly use the number instead.

5. Build and Run the whole project again.