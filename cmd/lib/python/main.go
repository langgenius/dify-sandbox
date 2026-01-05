package main

import (
	"fmt"
	"os"

	"github.com/langgenius/dify-sandbox/internal/core/lib/python"
)
import "C"

//export DifySeccomp
func DifySeccomp(uid int, gid int, enable_network bool) {
	err := python.InitSeccomp(uid, gid, enable_network)
	if err != nil {
		fmt.Fprintf(os.Stderr, "InitSeccomp failed: %v\n", err)
		os.Exit(1)
	}
}

func main() {}
