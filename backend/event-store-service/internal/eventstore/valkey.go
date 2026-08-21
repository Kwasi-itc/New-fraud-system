package eventstore

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// valkeyClient intentionally implements only the small RESP subset used by the
// bounded feature cache. The event store remains correct when Valkey is absent.
type valkeyClient struct {
	address string
	timeout time.Duration
	pool    *sync.Pool
}

type pooledValkeyConnection struct {
	net.Conn
	reader *bufio.Reader
}

func newValkeyClient(address string, timeout time.Duration) valkeyClient {
	return valkeyClient{address: address, timeout: timeout, pool: &sync.Pool{}}
}

func (c valkeyClient) command(ctx context.Context, args ...string) (value string, err error) {
	if strings.TrimSpace(c.address) == "" {
		return "", errors.New("valkey disabled")
	}
	var connection *pooledValkeyConnection
	if c.pool != nil {
		if cached := c.pool.Get(); cached != nil {
			connection, _ = cached.(*pooledValkeyConnection)
		}
	}
	if connection == nil {
		dialer := net.Dialer{Timeout: c.timeout}
		conn, dialErr := dialer.DialContext(ctx, "tcp", c.address)
		if dialErr != nil {
			return "", dialErr
		}
		connection = &pooledValkeyConnection{Conn: conn, reader: bufio.NewReader(conn)}
	}
	defer func() {
		if err != nil {
			_ = connection.Close()
			return
		}
		if c.pool != nil {
			c.pool.Put(connection)
			return
		}
		_ = connection.Close()
	}()
	deadline := time.Now().Add(c.timeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	_ = connection.SetDeadline(deadline)
	var request strings.Builder
	request.WriteString("*")
	request.WriteString(strconv.Itoa(len(args)))
	request.WriteString("\r\n")
	for _, arg := range args {
		request.WriteString("$")
		request.WriteString(strconv.Itoa(len(arg)))
		request.WriteString("\r\n")
		request.WriteString(arg)
		request.WriteString("\r\n")
	}
	if _, err = connection.Write([]byte(request.String())); err != nil {
		return "", err
	}
	value, err = readRESP(connection.reader)
	return value, err
}

func readRESP(reader *bufio.Reader) (string, error) {
	prefix, err := reader.ReadByte()
	if err != nil {
		return "", err
	}
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	switch prefix {
	case '+', ':':
		return line, nil
	case '-':
		return "", errors.New(line)
	case '$':
		length, err := strconv.Atoi(line)
		if err != nil {
			return "", err
		}
		if length < 0 {
			return "", nil
		}
		buffer := make([]byte, length+2)
		if _, err := io.ReadFull(reader, buffer); err != nil {
			return "", err
		}
		return string(buffer[:length]), nil
	default:
		return "", fmt.Errorf("unsupported RESP response %q", prefix)
	}
}
