package lib

import "testing"

func TestMergeSyscallsDedupesAndPreservesOrder(t *testing.T) {
	got := MergeSyscalls([]int{1, 2, 3}, []int{2, 4}, []int{3, 5})
	want := []int{1, 2, 3, 4, 5}

	if len(got) != len(want) {
		t.Fatalf("MergeSyscalls() len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("MergeSyscalls()[%d] = %d, want %d (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestParseSyscallNumbers(t *testing.T) {
	got := ParseSyscallNumbers(" 204, 302 ,,435 ")
	want := []int{204, 302, 435}

	if len(got) != len(want) {
		t.Fatalf("ParseSyscallNumbers() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ParseSyscallNumbers()[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestParseSyscallNumbersIgnoresInvalidEntries(t *testing.T) {
	got := ParseSyscallNumbers("204,abc,302")
	want := []int{204, 302}

	if len(got) != len(want) {
		t.Fatalf("ParseSyscallNumbers() = %v, want %v", got, want)
	}
}

func TestMergeSyscallsKeepsDefaultsWhenExtending(t *testing.T) {
	// read(0), write(1), and exit(60) are required for basic I/O.
	defaults := []int{0, 1, 60}
	custom := []int{204} // sched_getaffinity on amd64

	got := MergeSyscalls(defaults, custom)
	want := []int{0, 1, 60, 204}

	if len(got) != len(want) {
		t.Fatalf("MergeSyscalls() len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("MergeSyscalls()[%d] = %d, want %d (full: %v)", i, got[i], want[i], got)
		}
	}
}
