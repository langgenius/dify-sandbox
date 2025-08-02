package main

import (
	"github.com/langgenius/dify-sandbox/internal/core/lib/uv"
)
import "C"

//export DifySeccomp
func DifySeccomp(uid int, gid int, enable_network bool) {
	uv.InitSeccomp(uid, gid, enable_network)
}

func main() {}
