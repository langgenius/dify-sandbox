package main

import (
	"fmt"
	"os"
	"os/exec"
)

const (
	SYSCALL_NUMS = 400
)

func main() {
	for i := 0; i < SYSCALL_NUMS; i++ {
		os.Setenv("DISABLE_SYSCALL", fmt.Sprintf("%d", i))
		_, err := exec.Command("python3", ".test.py").Output()
		if err != nil {
			fmt.Printf("%d,", i)
		}
	}
}
