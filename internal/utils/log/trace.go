package log

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
)

func ParseTraceparent(header string) (traceID, spanID string, ok bool) {
	if header == "" {
		return "", "", false
	}

	parts := strings.Split(header, "-")
	if len(parts) != 4 {
		return "", "", false
	}

	version := parts[0]
	traceID = parts[1]
	spanID = parts[2]

	if version != "00" {
		return "", "", false
	}

	if len(traceID) != 32 || !isHex(traceID) {
		return "", "", false
	}

	if len(spanID) != 16 || !isHex(spanID) {
		return "", "", false
	}

	return traceID, spanID, true
}

func GenerateTraceID() string {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		return strings.Repeat("0", 32)
	}
	return hex.EncodeToString(b)
}

func GenerateSpanID() string {
	b := make([]byte, 8)
	_, err := rand.Read(b)
	if err != nil {
		return strings.Repeat("0", 16)
	}
	return hex.EncodeToString(b)
}

func isHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func GetTraceparentHeader(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	tc, ok := TraceFromContext(ctx)
	if !ok || tc.TraceID == "" {
		return ""
	}
	return "00-" + tc.TraceID + "-" + tc.SpanID + "-01"
}
