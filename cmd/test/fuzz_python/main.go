package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/langgenius/dify-sandbox/internal/static/python_syscall"
)

const (
	SYSCALL_NUMS = 400
)

func run(allowed_syscalls []int) {
	nums := []string{}
	for _, syscall := range allowed_syscalls {
		nums = append(nums, strconv.Itoa(syscall))
	}
	os.Setenv("ALLOWED_SYSCALLS", strings.Join(nums, ","))
	_, err := exec.Command("python3", "cmd/test/fuzz_python/test.py").Output()
	if err == nil {
		//fmt.Println(string(p))
	} else {
		fmt.Println(err)
	}
}

func find_syscall(syscall int, syscalls []int) int {
	for i, s := range syscalls {
		if s == syscall {
			return i
		}
	}
	return -1
}

func main() {
	original := python_syscall.ALLOW_SYSCALLS
	original = append(original, python_syscall.ALLOW_NETWORK_SYSCALLS...)

	// generate task list
	list := make([][]int, SYSCALL_NUMS)
	for i := 0; i < SYSCALL_NUMS; i++ {
		list[i] = make([]int, len(original))
		copy(list[i], original)
		// add i
		if find_syscall(i, original) == -1 {
			list[i] = append(list[i], i)
		}

		for j := 49; j < 50; j++ {
			if find_syscall(j, list[i]) == -1 {
				list[i] = append(list[i], j)
			}
		}

		// for j := 293; j < 294; j++ {
		// 	if find_syscall(j, list[i]) == -1 {
		// 		list[i] = append(list[i], j)
		// 	}
		// }

		// for j := 220; j < 220; j++ {
		// 	if find_syscall(j, list[i]) == -1 {
		// 		list[i] = append(list[i], j)
		// 	}
		// }

		// for j := 100; j < 100; j++ {
		// 	if find_syscall(j, list[i]) == -1 {
		// 		list[i] = append(list[i], j)
		// 	}
		// }
	}

	lock := sync.Mutex{}
	wg := sync.WaitGroup{}
	i := 0

	// run 4 tasks concurrently
	for j := 0; j < 4; j++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				lock.Lock()
				if i >= len(list) {
					lock.Unlock()
					return
				}
				task := list[i]
				i++
				lock.Unlock()
				run(task)
			}
		}()
	}

	// wait for all tasks to finish
	wg.Wait()
}
