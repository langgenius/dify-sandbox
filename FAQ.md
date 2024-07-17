Here's the translated FAQ document in English, formatted in markdown for better readability:

---

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

So, when encountering such errors, you need to modify the `python_lib_path` in your `config.yaml` to include the shared object paths required by your Python code.

**Note:** The Go process initializes this environment at startup, so if you configure too many `python_lib_path`, the startup will be very slow. For serverless environments, consider modifying the code to complete this build in a Docker container.

### 2. My Python code returns an "operation not permitted" error?

`dify-sandbox` uses Linux seccomp to restrict system calls. It’s recommended to read the source code ([internal/core/lib/python/add_seccomp.go](https://github.com/langgenius/dify-sandbox/blob/main/internal/core/lib/python/add_seccomp.go)). When you encounter this error, it usually means your code executed a restricted system call. The default allowed system calls are configured in [syscalls_amd64](https://github.com/langgenius/dify-sandbox/blob/main/internal/static/python_syscall/syscalls_amd64.go). You can modify this according to your system’s needs (currently, it cannot be modified through the configuration file).

To quickly identify the system calls your Python code depends on, here are two recommended methods:

#### Method 1: Using `strace` to log all the system calls

1. Write a test Python file, for example, `test_numpy.py`, and add a line of code to import numpy:
    ```python
    import numpy as np
    ```

2. Use `strace` to log all the system calls:
    ```sh
    strace -o strace_output.txt -e trace=all python test_numpy.py
    ```

3. Use `awk` and `sort` to print all the system calls:
    ```sh
    awk '{print $1}' strace_output.txt | sed 's/[(].*//' | sort | uniq -c | sort -nr
    ```

    Then, get the list of system calls, diff, and add:
    ```
        831 stat
        418 fstat
        393 read
        337 lseek
        278 openat
        250 close
        215 mmap
        180 ioctl
         68 rt_sigaction
         60 mprotect
         54 getdents64
         42 brk
         35 futex
         18 pread64
         17 munmap
          7 clone
          6 lstat
          4 readlink
          3 uname
          3 dup
          2 shmget
          2 getuid
          2 getgid
          2 geteuid
          2 getegid
          2 getcwd
          2 arch_prctl
          1 sysinfo
          1 shmdt
          1 shmat
          1 set_tid_address
          1 set_robust_list
          1 sched_getaffinity
          1 rt_sigprocmask
          1 prlimit64
          1 gettid
          1 fcntl
          1 exit_group
          1 execve
          1 epoll_create1
          1 access
    ```

#### Method 2: Using `dify-sandbox` test code to scan and output all system calls

1. Modify the `/cmd/test/syscall_dig/test.py` Python code to **append** your test codes to the end of this file.
2. Run `go run cmd/test/syscall_dig/main.go` to get the required system calls.

---

