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
	SYSCALL_NUMS = 500
)

var (
	total_rans      = 0
	total_rans_lock = sync.Mutex{}
)

func run(allowed_syscalls []int) error {
	total_rans_lock.Lock()
	total_rans++
	total_rans_lock.Unlock()

	nums := []string{}
	for _, syscall := range allowed_syscalls {
		nums = append(nums, strconv.Itoa(syscall))
	}
	os.Setenv("ALLOWED_SYSCALLS", strings.Join(nums, ","))
	cmd := exec.Command("python3", "cmd/test/fuzz_python_amd64/test.py")
	cmd.Stderr = os.Stderr
	_, err := cmd.Output()
	if err == nil {
		return nil
	} else {
		return err
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

func fuzz_range(r []int) bool {
	original := python_syscall.ALLOW_SYSCALLS
	original = append(original, python_syscall.ALLOW_NETWORK_SYSCALLS...)

	// generate task list
	list := make([][]int, 10)
	for i := 0; i < 10; i++ {
		list[i] = make([]int, len(original))
		copy(list[i], original)
		for _, j := range r {
			if find_syscall(j, list[i]) == -1 {
				list[i] = append(list[i], j)
			}
		}
	}

	lock := sync.Mutex{}
	wg := sync.WaitGroup{}
	i := 0

	// run 4 tasks concurrently
	failed := false
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
				if run(task) != nil {
					failed = true
					// stop all tasks
					lock.Lock()
					i = len(list)
					lock.Unlock()
				}
			}
		}()
	}

	// wait for all tasks to finish
	wg.Wait()

	return !failed
}

func fuzz() {
	unnecessary_syscalls := []int{}

	for i := 0; i < SYSCALL_NUMS; i++ {
		// remove syscall
		fmt.Println(i)
		nr := []int{}
		for j := 0; j < SYSCALL_NUMS; j++ {
			// skip i and unnecessary syscalls
			if i != j && find_syscall(j, unnecessary_syscalls) == -1 &&
				find_syscall(j, python_syscall.ALLOW_SYSCALLS) == -1 &&
				find_syscall(j, python_syscall.ALLOW_NETWORK_SYSCALLS) == -1 {
				nr = append(nr, j)
			}
		}

		if fuzz_range(nr) {
			unnecessary_syscalls = append(unnecessary_syscalls, i)
		} else {
			fmt.Printf("syscall %d is necessary\n", i)
		}
	}
}

func main() {
	fuzz()
	fmt.Printf("Total rans: %d\n", total_rans)
}
