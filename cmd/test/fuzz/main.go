package main

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
)

const (
	SYSCALL_NUMS = 400
)

func run(i int) {
	os.Setenv("DISABLE_SYSCALL", fmt.Sprintf("%d", i))
	_, err := exec.Command("node", "test.js").Output()
	if err != nil {
		fmt.Println(i)
	}
}

func main() {
	os.Chdir(".node_temp")
	// generate task list
	list := make([]int, SYSCALL_NUMS)
	for i := 0; i < SYSCALL_NUMS; i++ {
		list[i] = i
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
