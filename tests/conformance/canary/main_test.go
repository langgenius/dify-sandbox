package main

import (
	"io"
	"net"
	"testing"
)

func TestCollectorCountsReadsAndActualConnectionLifetime(t *testing.T) {
	counters := newCounters([]string{"tcp/192.0.2.10/19001"})
	left, right := net.Pipe()
	tracked := counters.accept("tcp/192.0.2.10/19001", left)
	initial := counters.snapshot()
	if initial.Counters["tcp/192.0.2.10/19001"] != 1 || initial.Active["tcp/192.0.2.10/19001"] != 1 {
		t.Fatal(initial)
	}
	done := make(chan struct{})
	go func() { right.Write([]byte("observed")); close(done) }()
	buffer := make([]byte, 8)
	if _, err := io.ReadFull(tracked, buffer); err != nil {
		t.Fatal(err)
	}
	<-done
	observed := counters.snapshot()
	if observed.ReceivedBytes["tcp/192.0.2.10/19001"] != 8 {
		t.Fatal("receipt did not reflect actual reads")
	}
	tracked.Close()
	tracked.Close()
	right.Close()
	closed := counters.snapshot()
	if closed.Active["tcp/192.0.2.10/19001"] != 0 || closed.Counters["tcp/192.0.2.10/19001"] != 1 {
		t.Fatal("close accounting incorrect", closed)
	}
	if initial.BootID == "" || initial.BootID != closed.BootID {
		t.Fatal("unstable boot identity")
	}
}

func TestCollectorDatagramsDoNotInventActiveConnections(t *testing.T) {
	counters := newCounters([]string{"udp/192.0.2.10/19002"})
	counters.datagram("udp/192.0.2.10/19002", 19)
	observed := counters.snapshot()
	if observed.Active["udp/192.0.2.10/19002"] != 0 || observed.Counters["udp/192.0.2.10/19002"] != 1 || observed.ReceivedBytes["udp/192.0.2.10/19002"] != 19 {
		t.Fatal(observed)
	}
}
