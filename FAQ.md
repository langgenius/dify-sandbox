## FAQ

### 1. Why does my Python code throw an exception like "xxx.so: cannot open shared object file: No such file or directory"?

This occurs because the `dify-sandbox` implementation generates a temporary file in the `/var/sandbox/sandbox-python/` directory to save and execute your Python code. Before running your Python code, it uses `syscall.Chroot` to restrict the current process's root to the `/var/sandbox/sandbox-python/` directory. This directory structure, visible to the Python process, determines all the Python modules/packages that can be imported, including modules based on C code.

- Root: `/var/sandbox/sandbox-python/` is the root directory from the Python process perspective. Its subdirectories are now discovered automatically from the configured `python_path` before chroot. Usually, it includes:
  - `etc/` directory
  - `python.so` shared object, compiled and built by `dify-sandbox`
  - `usr/lib` directory
  - `usr/local/`

At startup, `dify-sandbox` runs the configured interpreter from `python_path` and discovers its stdlib, site-packages, and absolute `sys.path` entries. It then appends a small fixed set of system files such as certificate bundles, resolver config, timezone data, and the platform library directory.

`python_lib_path` and `PYTHON_LIB_PATH` are no longer used. If discovery fails, fix `python_path` or the Python installation in your image so that running `python_path -c 'import json'` succeeds before the sandbox starts.

If you still see missing shared-library errors, install the package or shared object into the same Python environment referenced by `python_path`, or into the system library locations that are already copied into the sandbox. Do not try to work around this by configuring extra sandbox copy paths.

### 2. My Python code returns an "operation not permitted" error?

`dify-sandbox` uses Linux seccomp to restrict system calls. It’s recommended to read the source code ([internal/core/lib/python/add_seccomp.go](https://github.com/langgenius/dify-sandbox/blob/main/internal/core/lib/python/add_seccomp.go)). When you encounter this error, it usually means your code executed a restricted system call. The default allowed system calls are configured in [syscalls_amd64](https://github.com/langgenius/dify-sandbox/blob/main/internal/static/python_syscall/syscalls_amd64.go). You can modify this according to your system’s needs (currently, it cannot be modified through the configuration file).

To quickly identify the system calls your Python code depends on, here is the recommended method:

1. Modify the `/cmd/test/syscall_dig/test.py`, add your own code besides or in the `main` function. For example, you can add `import numpy` before `main`.

2. Run `go run cmd/test/syscall_dig/main.go`, the output will like this:
```shell
~/dify-sandbox$ # make sure you already build this project, there should be a file `internal/core/runner/python/python.so`
~/dify-sandbox$ mkdir -p /var/sandbox/sandbox-python && cp internal/core/runner/python/python.so /var/sandbox/sandbox-python/
~/dify-sandbox$ go run cmd/test/syscall_dig/main.go
failed with signal: bad system call
...
failed with signal: bad system call
Following syscalls are required: 0,1,3,5,8,9,10,11,12,13,14,15,16,17,24,28,35,39,60,63,105,106,131,186,202,204,217,231,233,234,237,257,262,273,281,291,318,334,435
```
If you haven't got output like this format, maybe it's your permission problem, try run it with `sudo` again.

In case you get an output saying **Failed to get the needed syscalls**, here is another way you may try:
```shell
# install the `strace` command by yourself
strace -c python cmd/test/syscall_dig/test.py
```
Make sure the Python script runs successfully. Then copy the output of the `strace` command. It looks like the following format:
```
% time     seconds  usecs/call     calls    errors syscall 
------ ----------- ----------- --------- --------- ---------------- 
26.37    0.023713          11      2010       216 stat 
14.65    0.013177          13       996           read 
10.42    0.009376           8      1076           fstat
...           ...         ...       ...             ...
------ ----------- ----------- --------- --------- ---------------- 
100.00    0.089940                  8002       690 total 
```
Also copy the `ALLOW_SYSCALLS` variable in the `internal/static/python_syscall/syscalls_amd64.go` file. Feed these copies into LLM (such as gpt-4o, gemini-2.0-flash, etc.). Let the LLM help you decide how to edit the `ALLOW_SYSCALLS` variable.
A prompt example:
```
Here is the output of `strace -c` which show all the system calls during execution:
${strace_output}

Edit the following `ALLOW_SYSCALLS` variable, Add all the above system calls to this variable. Note that do not remove the existing values in it. Only add new ones at the end.
var ALLOW_SYSCALLS = ${ALLOW_SYSCALLS_LIST}
```

3. These syscalls are the sandbox already added: `0,1,3,8,9,10,11,12,13,14,15,16,16,24,25,35,39,60,96,102,105,106,110,131,186,201,202,217,228,230,231,233,234,257,262,270,273,291,318,334`. You need to compare what is the extras syscall numbers of previous step. You can use a simple script or ask LLM to archive that. In this case, it's `5, 17, 28, 63, 204, 237, 281, 435`

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
If the syscall alias is not defined in golang, you can directly use the number instead.

5. Build and Run the whole project again.
