package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
)

const (
	SYSCALL_NUMS = 400
)

func main() {
	// copy ./internal/core/runner/python/python.so to /tmp/sandbox-python/python.so
	os.MkdirAll("/tmp/sandbox-python", 0755)
	f1, err := os.Create("/tmp/sandbox-python/python.so")
	if err != nil {
		fmt.Println(err)
		return
	}
	f2, err := os.Open("./internal/core/runner/python/python.so")
	if err != nil {
		fmt.Println(err)
		return
	}
	io.Copy(f1, f2)
	f1.Close()
	f2.Close()

	for i := 0; i < SYSCALL_NUMS; i++ {
		os.Setenv("DISABLE_SYSCALL", fmt.Sprintf("%d", i))
		var err error
		var jobs = make(chan int, 100)
		var wg sync.WaitGroup
		for j := 0; j < 4; j++ {
			wg.Add(1)
			i := i
			go func() {
				defer wg.Done()
				for range jobs {
					if err != nil {
						continue
					}
					_, err = exec.Command("python3", ".fuzz.py").Output()
					if err != nil {
						fmt.Println(i)
					}
				}
			}()
		}

		for j := 0; j < 100; j++ {
			jobs <- j
		}

		close(jobs)
		wg.Wait()
	}
}
