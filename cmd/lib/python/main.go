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

func main() {}
