package integrationtests_test

import (
	"sync"
	"testing"
)

func runMultipleTestings(t *testing.T, iterations int, testFunc func(*testing.T)) {
	for i := 0; i < iterations; i++ {
		testFunc(t)
	}
}

func runConcurrentTestings(t *testing.T, workers int, iterations int, testFunc func(int) error) {
	t.Helper()

	var wg sync.WaitGroup
	errCh := make(chan error, workers*iterations)

	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				if err := testFunc(workerID*iterations + i); err != nil {
					errCh <- err
				}
			}
		}(worker)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Error(err)
	}
}
