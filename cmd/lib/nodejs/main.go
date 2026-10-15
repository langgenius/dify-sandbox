package main

import "github.com/langgenius/dify-sandbox/internal/core/lib/nodejs"
import "C"

//export DifySeccomp
func DifySeccomp(uid int, gid int, enable_network bool) {
	if err := nodejs.InitSeccomp(uid, gid, enable_network); err != nil {
		panic(err)
	}
}

//export DifySeccompIsolated
func DifySeccompIsolated(uid int, gid int, enable_network bool) {
	if enable_network {
		panic("isolated network scope is not configured")
	}
	if err := nodejs.InitSeccompIsolated(uid, gid); err != nil {
		panic(err)
	}
}

//export DifySeccompIsolatedScoped
func DifySeccompIsolatedScoped(uid int, gid int, enable_network bool) {
	if !enable_network {
		panic("scoped client mode required")
	}
	if err := nodejs.InitSeccompIsolatedScoped(uid, gid); err != nil {
		panic(err)
	}
}
func main() {}
