//go:build linux && arm64

package static

var DEFAULT_PYTHON_LIB_REQUIREMENTS = []string{
	"/usr/local/lib/python3.10",
	"/usr/lib/python3.10",
	"/usr/lib/python3",
	"/etc/ssl/certs/ca-certificates.crt",
	"/etc/nsswitch.conf",
	"/etc/resolv.conf",
	"/run/systemd/resolve/stub-resolv.conf",
	"/run/resolvconf/resolv.conf",
	"/usr/lib/aarch64-linux-gnu/libssl.so.3",
	"/usr/lib/aarch64-linux-gnu/libcrypto.so.3",
	"/etc/hosts",
}
