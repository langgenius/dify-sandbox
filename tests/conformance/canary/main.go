// Controlled traffic collector for a disposable, route-less outer namespace.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/langgenius/dify-sandbox/tests/conformance/cases"
	"golang.org/x/net/dns/dnsmessage"
)

type counters struct {
	mu                     sync.Mutex
	boot                   string
	hits, active, received map[string]uint64
}

func newCounters(keys []string) *counters {
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		panic(err)
	}
	c := &counters{boot: hex.EncodeToString(token), hits: map[string]uint64{}, active: map[string]uint64{}, received: map[string]uint64{}}
	for _, key := range keys {
		c.hits[key] = 0
		c.active[key] = 0
		c.received[key] = 0
	}
	return c
}
func (c *counters) snapshot() cases.NetworkCounters {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := cases.NetworkCounters{BootID: c.boot, Counters: map[string]uint64{}, Active: map[string]uint64{}, ReceivedBytes: map[string]uint64{}}
	for key, value := range c.hits {
		result.Counters[key] = value
		result.Active[key] = c.active[key]
		result.ReceivedBytes[key] = c.received[key]
	}
	return result
}
func (c *counters) datagram(key string, size int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hits[key]++
	c.received[key] += uint64(size)
}
func (c *counters) accept(key string, connection net.Conn) net.Conn {
	c.mu.Lock()
	c.hits[key]++
	c.active[key]++
	c.mu.Unlock()
	return &countedConn{Conn: connection, owner: c, key: key}
}

type countedConn struct {
	net.Conn
	owner *counters
	key   string
	once  sync.Once
}

func (c *countedConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	c.owner.mu.Lock()
	c.owner.received[c.key] += uint64(n)
	c.owner.mu.Unlock()
	return n, err
}
func (c *countedConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() { c.owner.mu.Lock(); c.owner.active[c.key]--; c.owner.mu.Unlock() })
	return err
}

type countedListener struct {
	net.Listener
	owner *counters
	key   string
}

func (l *countedListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return l.owner.accept(l.key, c), nil
}
func targetKey(t cases.NetworkTarget) string {
	return fmt.Sprintf("%s/%s/%d", t.Protocol, t.Address, t.Port)
}
func targetAddress(t cases.NetworkTarget) string {
	return net.JoinHostPort(t.Address, strconv.Itoa(int(t.Port)))
}
func certificate() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "controlled-fixture"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	raw, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: raw}))
}
func handler(alternate cases.NetworkTarget) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, http.MaxBytesReader(w, r.Body, 1<<20)); err != nil {
			http.Error(w, "body too large", 413)
			return
		}
		switch r.URL.Path {
		case "/ok":
			w.Write([]byte("OK"))
		case "/redirect-unapproved":
			http.Redirect(w, r, alternate.Protocol+"://"+targetAddress(alternate)+"/ok", 302)
		case "/bytes":
			count, err := strconv.Atoi(r.URL.Query().Get("count"))
			if err != nil || count < 0 || count > 1<<20 {
				http.Error(w, "bounded count required", 400)
				return
			}
			w.Write(make([]byte, count))
		case "/hold":
			w.WriteHeader(200)
			w.(http.Flusher).Flush()
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()
			timer := time.NewTimer(15 * time.Second)
			defer timer.Stop()
			for {
				select {
				case <-r.Context().Done():
					return
				case <-timer.C:
					return
				case <-ticker.C:
					if _, err := w.Write([]byte("h")); err != nil {
						return
					}
					w.(http.Flusher).Flush()
				}
			}
		default:
			http.NotFound(w, r)
		}
	})
}
func main() {
	config := flag.String("config", "/qualification/network.json", "protected fixture target inventory")
	flag.Parse()
	raw, err := os.ReadFile(*config)
	if err != nil {
		log.Fatal(err)
	}
	var f cases.NetworkFixture
	if err = json.Unmarshal(raw, &f); err != nil {
		log.Fatal(err)
	}
	all := append(append([]cases.NetworkTarget{}, f.Targets...), f.AlternateTargets...)
	keys := []string{}
	for _, t := range all {
		keys = append(keys, targetKey(t))
	}
	counts := newCounters(keys)
	cert, err := certificate()
	if err != nil {
		log.Fatal(err)
	}
	var dnsMu sync.Mutex
	dnsSeen := map[dnsmessage.Type]int{}
	for _, target := range all {
		t := target
		key := targetKey(t)
		if t.Protocol == "dns" || t.Protocol == "udp" {
			socket, err := net.ListenPacket("udp", targetAddress(t))
			if err != nil {
				log.Fatal(err)
			}
			go func() {
				buffer := make([]byte, 65536)
				for {
					n, peer, err := socket.ReadFrom(buffer)
					if err != nil {
						log.Fatal(err)
					}
					counts.datagram(key, n)
					if t.Protocol == "udp" {
						socket.WriteTo([]byte("OK"), peer)
						continue
					}
					var query dnsmessage.Message
					if query.Unpack(buffer[:n]) != nil || len(query.Questions) != 1 {
						continue
					}
					question := query.Questions[0]
					response := dnsmessage.Message{Header: dnsmessage.Header{ID: query.ID, Response: true, Authoritative: true}, Questions: query.Questions}
					if question.Name.String() == "rebind.fixture.test." {
						dnsMu.Lock()
						dnsSeen[question.Type]++
						sequence := dnsSeen[question.Type]
						dnsMu.Unlock()
						address := "192.0.2.10"
						if sequence > 1 {
							address = "192.0.2.20"
						}
						header := dnsmessage.ResourceHeader{Name: question.Name, Type: question.Type, Class: dnsmessage.ClassINET, TTL: 0}
						if question.Type == dnsmessage.TypeAAAA {
							address = "2001:db8::10"
							if sequence > 1 {
								address = "2001:db8::20"
							}
							var a [16]byte
							copy(a[:], net.ParseIP(address).To16())
							response.Answers = []dnsmessage.Resource{{Header: header, Body: &dnsmessage.AAAAResource{AAAA: a}}}
						} else if question.Type == dnsmessage.TypeA {
							var a [4]byte
							copy(a[:], net.ParseIP(address).To4())
							response.Answers = []dnsmessage.Resource{{Header: header, Body: &dnsmessage.AResource{A: a}}}
						}
					}
					packet, err := response.Pack()
					if err == nil {
						socket.WriteTo(packet, peer)
					}
				}
			}()
			continue
		}
		listener, err := net.Listen("tcp", targetAddress(t))
		if err != nil {
			log.Fatal(err)
		}
		var tracked net.Listener = &countedListener{Listener: listener, owner: counts, key: key}
		if t.Protocol == "tcp" {
			go func() {
				for {
					connection, err := tracked.Accept()
					if err != nil {
						log.Fatal(err)
					}
					go func() {
						defer connection.Close()
						connection.SetDeadline(time.Now().Add(15 * time.Second))
						io.Copy(io.Discard, io.LimitReader(connection, 1<<20))
					}()
				}
			}()
			continue
		}
		if t.Protocol == "https" {
			tracked = tls.NewListener(tracked, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
		}
		alternate := t
		for _, a := range f.AlternateTargets {
			if a.Protocol == t.Protocol && (net.ParseIP(a.Address).To4() != nil) == (net.ParseIP(t.Address).To4() != nil) {
				alternate = a
			}
		}
		server := &http.Server{Handler: handler(alternate), ReadHeaderTimeout: time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 16 * time.Second, IdleTimeout: time.Second, MaxHeaderBytes: 16 << 10}
		go func() { log.Fatal(server.Serve(tracked)) }()
	}
	// Open this listener last. A collector response proves every target was bound.
	collector := &http.Server{Addr: "127.0.0.1:19000", ReadHeaderTimeout: time.Second, WriteTimeout: time.Second, MaxHeaderBytes: 1024, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(counts.snapshot())
	})}
	log.Fatal(collector.ListenAndServe())
}
