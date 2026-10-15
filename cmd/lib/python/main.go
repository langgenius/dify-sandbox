package main

import (
	"github.com/langgenius/dify-sandbox/internal/core/lib/python"
)
import "C"

//export DifySeccomp
func DifySeccomp(uid int, gid int, enable_network bool) {
	if err := python.InitSeccomp(uid, gid, enable_network); err != nil {
		panic(err)
	}
}

//export DifySeccompIsolated
func DifySeccompIsolated(uid int, gid int, enable_network bool) {
	if enable_network {
		panic("isolated network scope is not configured")
	}
	if err := python.InitSeccompIsolated(uid, gid); err != nil {
		panic(err)
	}
}

//export DifySeccompIsolatedScoped
func DifySeccompIsolatedScoped(uid int, gid int, enable_network bool) {
	if !enable_network {
		panic("scoped client mode required")
	}
	if err := python.InitSeccompIsolatedScoped(uid, gid); err != nil {
		panic(err)
	}
}
func main() {}
