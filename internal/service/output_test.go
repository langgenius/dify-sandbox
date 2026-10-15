package service

import (
	"strings"
	"testing"
)

func TestCollectorCannotReturnPartialSuccess(t *testing.T) {
	out := make(chan []byte, 1)
	errout := make(chan []byte, 1)
	done := make(chan bool, 1)
	out <- []byte(strings.Repeat("x", 20))
	done <- true
	result := collectOutput(out, errout, done, 4)
	if result.Code != -500 {
		t.Fatal("partial output succeeded", result)
	}
}
