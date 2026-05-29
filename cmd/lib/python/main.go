package main

import (
	"fmt"
	"os"

	"github.com/langgenius/dify-sandbox/internal/core/lib"
	"github.com/langgenius/dify-sandbox/internal/core/lib/python"
)

/*
#include <stdint.h>
*/
import "C"

//export DifySeccomp
func DifySeccomp(uid int, gid int, enable_network bool) C.int {
	err := python.InitSeccomp(uid, gid, enable_network)
	if err != nil {
		fmt.Fprintf(os.Stderr, "python DifySeccomp error: %v\n", err)
		if coder, ok := err.(lib.ErrorCoder); ok {
			return C.int(coder.GetCode())
		}
		return C.int(lib.ERR_UNKNOWN)
	}
	return C.int(lib.SUCCESS)
}

func main() {}
