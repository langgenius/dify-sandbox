//go:build linux && arm64

package static

var DEFAULT_SYSTEM_LIB_REQUIREMENTS = []string{
	"/usr/lib/aarch64-linux-gnu",
	"/etc/ssl/certs/ca-certificates.crt",
	"/etc/nsswitch.conf",
	"/etc/hosts",
	"/etc/resolv.conf",
	"/run/systemd/resolve/stub-resolv.conf",
	"/run/resolvconf/resolv.conf",
	"/etc/localtime",
	"/usr/share/zoneinfo",
	"/etc/timezone",
}
