package mysql

import (
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// commitFaultProxy is restricted to the isolated plaintext MySQL test network.
// It drops either the COMMIT command or its server acknowledgement. All SQL,
// authorization, transactions and reconciliation still use the real owners.
func commitFaultProxy(t *testing.T, upstream string, afterCommit bool) (string, <-chan struct{}) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	fired := make(chan struct{})
	var injected atomic.Bool
	var connections sync.Map
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer client.Close()
				connections.Store(client, client)
				defer connections.Delete(client)
				server, err := net.DialTimeout("tcp", upstream, 3*time.Second)
				if err != nil {
					return
				}
				defer server.Close()
				_ = client.SetDeadline(time.Now().Add(30 * time.Second))
				_ = server.SetDeadline(time.Now().Add(30 * time.Second))
				var waiting atomic.Bool
				done := make(chan struct{})
				go func() {
					defer close(done)
					defer server.Close()
					defer client.Close()
					for {
						packet, err := readMySQLFaultPacket(client)
						if err != nil {
							return
						}
						if len(packet) > 5 && packet[4] == 3 && strings.EqualFold(string(packet[5:]), "COMMIT") && injected.CompareAndSwap(false, true) {
							if !afterCommit {
								close(fired)
								return
							}
							waiting.Store(true)
						}
						if _, err := server.Write(packet); err != nil {
							return
						}
					}
				}()
				for {
					packet, err := readMySQLFaultPacket(server)
					if err != nil {
						break
					}
					if waiting.Load() {
						// Require an actual server OK before losing its response.
						if len(packet) > 4 && packet[4] == 0 {
							close(fired)
						}
						break
					}
					if _, err := client.Write(packet); err != nil {
						break
					}
				}
				client.Close()
				server.Close()
				<-done
			}()
		}
	}()
	t.Cleanup(func() {
		listener.Close()
		connections.Range(func(_, value any) bool { value.(net.Conn).Close(); return true })
		wg.Wait()
	})
	return listener.Addr().String(), fired
}

func readMySQLFaultPacket(r io.Reader) ([]byte, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, err
	}
	size := int(header[0]) | int(header[1])<<8 | int(header[2])<<16
	packet := make([]byte, 4+size)
	copy(packet, header)
	_, err := io.ReadFull(r, packet[4:])
	return packet, err
}
