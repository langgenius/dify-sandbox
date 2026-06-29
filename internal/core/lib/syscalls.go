package lib

import (
	"os"
	"strconv"
	"strings"
)

func ParseSyscallNumbers(value string) []int {
	if value == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		n, err := strconv.Atoi(part)
		if err != nil {
			continue
		}
		out = append(out, n)
	}

	return out
}

func SyscallsFromEnv(key string) []int {
	return ParseSyscallNumbers(os.Getenv(key))
}

func MergeSyscalls(lists ...[]int) []int {
	seen := make(map[int]struct{})
	out := make([]int, 0)

	for _, list := range lists {
		for _, n := range list {
			if _, ok := seen[n]; ok {
				continue
			}
			seen[n] = struct{}{}
			out = append(out, n)
		}
	}

	return out
}
