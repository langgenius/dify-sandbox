package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/langgenius/dify-sandbox/internal/static/nodejs_syscall"
)

const (
	SYSCALL_NUMS = 1000
)

func run(allowed_syscalls []int) {
	os.Chdir("/tmp/sandbox-2c8a41ec-04ae-4209-8ed7-17bd476803a6/tmp/sandbox-nodejs-project/node_temp/node_temp")

	nums := []string{}
	for _, syscall := range allowed_syscalls {
		nums = append(nums, strconv.Itoa(syscall))
	}
	os.Setenv("ALLOWED_SYSCALLS", strings.Join(nums, ","))
	_, err := exec.Command("node", "test.js", "65537", "1001", "{\"enable_network\":true}").Output()
	if err == nil {
	} else {
		fmt.Println(err.Error())
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
	original := nodejs_syscall.ALLOW_SYSCALLS
	original = append(original, nodejs_syscall.ALLOW_NETWORK_SYSCALLS...)
	original = append(original, nodejs_syscall.ALLOW_ERROR_SYSCALLS...)

	// generate task list
	list := make([][]int, SYSCALL_NUMS)
	for i := 0; i < SYSCALL_NUMS; i++ {
		list[i] = make([]int, len(original))
		copy(list[i], original)
		// add i
		if find_syscall(i, original) == -1 {
			list[i] = append(list[i], i)
		}

		// for j := 15; j < 16; j++ {
		// 	if find_syscall(j, list[i]) == -1 {
		// 		list[i] = append(list[i], j)
		// 	}
		// }

		// for j := 24; j < 25; j++ {
		// 	if find_syscall(j, list[i]) == -1 {
		// 		list[i] = append(list[i], j)
		// 	}
		// }

		// for j := 60; j < 61; j++ {
		// 	if find_syscall(j, list[i]) == -1 {
		// 		list[i] = append(list[i], j)
		// 	}
		// }

		// for j := 186; j < 187; j++ {
		// 	if find_syscall(j, list[i]) == -1 {
		// 		list[i] = append(list[i], j)
		// 	}
		// }

		// for j := 204; j < 205; j++ {
		// 	if find_syscall(j, list[i]) == -1 {
		// 		list[i] = append(list[i], j)
		// 	}
		// }

		// for j := 273; j < 274; j++ {
		// 	if find_syscall(j, list[i]) == -1 {
		// 		list[i] = append(list[i], j)
		// 	}
		// }

		// for j := 334; j < 335; j++ {
		// 	if find_syscall(j, list[i]) == -1 {
		// 		list[i] = append(list[i], j)
		// 	}
		// }

		// for j := 435; j < 436; j++ {
		// 	if find_syscall(j, list[i]) == -1 {
		// 		list[i] = append(list[i], j)
		// 	}
		// }
	}

	lock := sync.Mutex{}
	wg := sync.WaitGroup{}
	i := 0

	// run 4 tasks concurrently
	for j := 0; j < 10; j++ {
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
