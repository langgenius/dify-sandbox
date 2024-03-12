package main

import "github.com/langgenius/dify-sandbox/internal/core/lib/nodejs"
import "C"

//export DifySeccomp
func DifySeccomp() {
	nodejs.InitSeccomp()
}

func main() {}
