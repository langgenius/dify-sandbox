package environment

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestContextVerificationPreservesDigestAndRejectsCancellation(t *testing.T) {
	root, m := writeTestEnvironment(t)
	got, err := LoadAndVerifyContext(context.Background(), root, m.Digest)
	if err != nil || got != m {
		t.Fatalf("context changed artifact binding: %+v %v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := LoadAndVerifyContext(ctx, root, m.Digest); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled load: %v", err)
	}
	if _, err := buildManifest(ctx, root, m); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled walk: %v", err)
	}
}

type cancelReader struct {
	cancel context.CancelFunc
	reads  int
}

func (r *cancelReader) Read(p []byte) (int, error) { r.reads++; r.cancel(); p[0] = 'a'; return 1, nil }
func TestHashReadChecksCancellationBetweenReads(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := &cancelReader{cancel: cancel}
	var out strings.Builder
	n, err := io.Copy(&out, contextReader{ctx: ctx, reader: r})
	if !errors.Is(err, context.Canceled) || n != 1 || r.reads != 1 {
		t.Fatalf("hash continued after cancellation: n=%d reads=%d err=%v", n, r.reads, err)
	}
}
