package mysql

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// Plaintext proxy is test-only on the isolated Compose network. It drops one
// actual COMMIT OK packet, not a fabricated application response.
type commitProxy struct {
	address            string
	arm, observed      atomic.Bool
	finalOnly, offline atomic.Bool
	disconnect         func()
}

func startCommitProxy(t *testing.T) *commitProxy {
	t.Helper()
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	p := &commitProxy{address: listener.Addr().String()}
	var mu sync.Mutex
	connections := map[net.Conn]bool{}
	var wg sync.WaitGroup
	closed := make(chan struct{})
	p.disconnect = func() {
		mu.Lock()
		defer mu.Unlock()
		for c := range connections {
			c.Close()
		}
	}
	track := func(c net.Conn) { mu.Lock(); connections[c] = true; mu.Unlock() }
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			client, e := listener.Accept()
			if e != nil {
				return
			}
			if p.offline.Load() {
				client.Close()
				continue
			}
			track(client)
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer client.Close()
				server, e := net.Dial("tcp", "mysql:3306")
				if e != nil {
					return
				}
				track(server)
				defer server.Close()
				var pending atomic.Bool
				finalTransaction := false
				done := make(chan struct{})
				go func() {
					defer close(done)
					defer client.Close()
					defer server.Close()
					for {
						packet, e := mysqlPacket(server)
						if e != nil {
							return
						}
						if pending.Load() {
							pending.Store(false)
							if len(packet) > 4 && packet[4] == 0 {
								p.observed.Store(true)
								return
							}
						}
						if e = writePacket(client, packet); e != nil {
							return
						}
					}
				}()
				for {
					packet, e := mysqlPacket(client)
					if e != nil {
						break
					}
					if len(packet) > 5 && strings.Contains(string(packet[5:]), ".ingestion_work_resources SET result_payload=") {
						finalTransaction = true
					}
					if len(packet) > 5 && (!p.finalOnly.Load() || finalTransaction) && packet[4] == 3 && bytes.EqualFold(packet[5:], []byte("COMMIT")) && p.arm.CompareAndSwap(true, false) {
						pending.Store(true)
					}
					if e = writePacket(server, packet); e != nil {
						break
					}
				}
				server.Close()
				client.Close()
				<-done
			}()
		}
	}()
	t.Cleanup(func() {
		close(closed)
		listener.Close()
		mu.Lock()
		for c := range connections {
			c.Close()
		}
		mu.Unlock()
		wg.Wait()
	})
	return p
}
func mysqlPacket(r io.Reader) ([]byte, error) {
	var header [4]byte
	if _, e := io.ReadFull(r, header[:]); e != nil {
		return nil, e
	}
	length := int(binary.LittleEndian.Uint32(header[:]) & 0xffffff)
	if length > 16<<20 {
		return nil, fmt.Errorf("oversized fixture packet")
	}
	packet := make([]byte, 4+length)
	copy(packet, header[:])
	_, e := io.ReadFull(r, packet[4:])
	return packet, e
}
func writePacket(w io.Writer, p []byte) error {
	for len(p) > 0 {
		n, e := w.Write(p)
		if e != nil {
			return e
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		p = p[n:]
	}
	return nil
}
func TestProcessLostCommitAcknowledgement(t *testing.T) {
	_, f, cfg := processFixture(t)
	proxy := startCommitProxy(t)
	cfg["Address"] = proxy.address
	api := startProcess(t, cfg, false)
	proxy.arm.Store(true)
	data := expectHTTP(t, api, "POST", recordPath(f), "lost-ack", `{"object_id":"one","number":1}`, 503)
	if !proxy.observed.Load() || !bytes.Contains(data, []byte("commit_outcome_unknown")) {
		t.Fatal(proxy.observed.Load(), string(data))
	}
	replay := expectHTTP(t, api, "POST", recordPath(f), "lost-ack", `{"object_id":"one","number":1}`, 200)
	if !bytes.Contains(replay, []byte(`"replayed":true`)) {
		t.Fatal(string(replay))
	}
	if got := fullDurableCounts(t, f); got != (fullOutcomeCounts{Records: 1, PublicReceipts: 1, Audits: 1, Outbox: 1}) {
		t.Fatal(got)
	}
}
