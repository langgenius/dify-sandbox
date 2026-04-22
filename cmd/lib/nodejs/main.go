package main

import "github.com/langgenius/dify-sandbox/internal/core/lib/nodejs"
import "C"

//export DifySeccomp
func DifySeccomp(uid int, gid int, enable_network bool) {
	if err := nodejs.InitSeccomp(uid, gid, enable_network); err != nil {
		panic(err)
	}
}

func main() {}
