package python

var (
	PYTHON_RUNNER_PATH = []string{
		"/usr/bin/python3",
		"/usr/bin/python3.10",
		"/usr/lib/python3/dist-packages",
		"/usr/lib/python3.10",
		"/usr/bin/echo",
		// libc
		"/lib/x86_64-linux-gnu/libc.so.6",
		"/lib/x86_64-linux-gnu/libm.so.6",
		"/lib/x86_64-linux-gnu/libexpat.so.1",
		"/lib/x86_64-linux-gnu/libz.so.1",
		"/lib/x86_64-linux-gnu/ld-linux-x86-64.so.2",
		"/lib/x86_64-linux-gnu/libexpat.so.1.8.7",
		"/lib/x86_64-linux-gnu/libz.so.1.2.11",
		"/lib64/ld-linux-x86-64.so.2",
		// libpthread
		"/lib/x86_64-linux-gnu/libpthread.so.0",
		// libdl
		"/lib/x86_64-linux-gnu/libdl.so.2",
	}
)
