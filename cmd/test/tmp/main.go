package main

import (
	"fmt"
	"os/exec"
)

func main() {
	cmd := exec.Command("python3", ".test.py")
	cmd.Env = []string{}
	reader, _ := cmd.StdoutPipe()

	stderr_reader, _ := cmd.StderrPipe()

	cmd.Start()

	go func() {
		for {
			buf := make([]byte, 1024)
			n, _ := reader.Read(buf)
			if n == 0 {
				break
			}
			print(string(buf))
		}
	}()

	go func() {
		for {
			buf := make([]byte, 1024)
			n, _ := stderr_reader.Read(buf)
			if n == 0 {
				break
			}
			print(string(buf))
		}
	}()

	err := cmd.Wait()

	if err != nil {
		fmt.Println(err.Error())
	}
}
