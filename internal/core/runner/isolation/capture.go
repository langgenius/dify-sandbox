package isolation

import (
	"bytes"
	"context"
	"io"
	"strings"
	"unicode/utf8"
)

type CaptureResult struct {
	Stdout, Stderr []byte
	Failure        string
}

// Capture retains at most each stream's byte budget. Blocking streams must be
// closable: cancellation closes them so a descendant cannot hold the drain open.
// Nonclosable in-memory readers are safe; arbitrary uninterruptible readers are
// refused before a goroutine can be stranded.
func Capture(ctx context.Context, stdout, stderr io.Reader, budget Budget) CaptureResult {
	return capture(ctx, stdout, stderr, budget, nil, nil)
}
func capture(ctx context.Context, stdout, stderr io.Reader, budget Budget, observe func([]byte), terminate func(string)) CaptureResult {
	if budget.StdoutBytes <= 0 || budget.StderrBytes <= 0 {
		return CaptureResult{Failure: "invalid_output"}
	}
	readers := []io.Reader{stdout, stderr}
	for _, r := range readers {
		switch r.(type) {
		case io.ReadCloser, *strings.Reader, *bytes.Reader, *bytes.Buffer:
		default:
			return CaptureResult{Failure: "invalid_output"}
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := func() {
		for _, r := range readers {
			if c, ok := r.(io.Closer); ok {
				c.Close()
			}
		}
	}
	defer stop()
	type stream struct {
		index   int
		data    []byte
		failure string
	}
	results := make(chan stream, 2)
	limits := []int64{budget.StdoutBytes, budget.StderrBytes}
	names := []string{"stdout_limit", "stderr_limit"}
	for i, r := range readers {
		go func(i int, r io.Reader) {
			s := stream{index: i}
			buf := make([]byte, 4096)
			for {
				n, err := r.Read(buf)
				if n > 0 {
					remaining := limits[i] - int64(len(s.data))
					take := n
					if int64(take) > remaining {
						take = int(remaining)
						s.failure = names[i]
					}
					s.data = append(s.data, buf[:take]...)
					if i == 0 && observe != nil && take > 0 {
						observe(buf[:take])
					}
					if s.failure != "" {
						if terminate != nil {
							terminate(s.failure)
						}
						results <- s
						return
					}
				}
				if err != nil {
					if err != io.EOF && ctx.Err() == nil {
						s.failure = "output_read"
					}
					break
				}
				if ctx.Err() != nil {
					break
				}
			}
			if s.failure == "" && !utf8.Valid(s.data) {
				s.failure = "invalid_output"
			}
			results <- s
		}(i, r)
	}
	var result CaptureResult
	for received := 0; received < 2; {
		select {
		case s := <-results:
			received++
			if s.index == 0 {
				result.Stdout = s.data
			} else {
				result.Stderr = s.data
			}
			if s.failure != "" && result.Failure == "" {
				result.Failure = s.failure
				cancel()
				stop()
			}
		case <-ctx.Done():
			if result.Failure == "" {
				result.Failure = "cancelled"
				if ctx.Err() == context.DeadlineExceeded {
					result.Failure = "deadline"
				}
			}
			stop()
			// Reads on the accepted blocking streams are interrupted by Close.
			for received < 2 {
				s := <-results
				received++
				if s.index == 0 {
					result.Stdout = s.data
				} else {
					result.Stderr = s.data
				}
			}
		}
	}
	return result
}
