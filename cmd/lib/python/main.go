package main

import "github.com/langgenius/dify-sandbox/internal/core/lib"
import "C"

//export DifySeccomp
func DifySeccomp() {
	lib.InitSeccomp()
}

func main() {}
