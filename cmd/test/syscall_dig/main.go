package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

const (
	SYSCALL_NUMS = 500
)

func run(allowed_syscalls []int) error {
	nums := []string{}
	for _, syscall := range allowed_syscalls {
		nums = append(nums, strconv.Itoa(syscall))
	}
	os.Setenv("ALLOWED_SYSCALLS", strings.Join(nums, ","))
	_, err := exec.Command("python3", "cmd/test/syscall_dig/test.py").Output()
	if err == nil {
	} else {
		failed_msg := fmt.Sprintf("failed with %v", err)
		fmt.Println(failed_msg)
	}

	return err
}

func main() {
	// generate all syscall list
	list := make([]int, 0, SYSCALL_NUMS)
	for i := 0; i < SYSCALL_NUMS; i++ {
		list = append(list, i)
	}

	for i := 0; i < SYSCALL_NUMS; i++ {
		syscall := list[0]
		list = list[1:]
		err := run(list)
		if err != nil {
			if strings.Contains(err.Error(), "bad system call") {
				// if run into err, then this syscall is needed, add it back to the end
				list = append(list, syscall)
			} else {
				fmt.Println(fmt.Sprintf("Failed to run your python code, %v", err))
			}
		}
	}

	// final test
	err := run(list)
	if err != nil {
		fmt.Println("Failed to get the needed syscalls")
	} else {
		// use ',' to join the list and print, easy for copy the list
		list_str := strings.Trim(strings.Join(strings.Fields(fmt.Sprint(list)), ","), "[]")
		fmt.Println("Following syscalls are required:", list_str)
	}
}
