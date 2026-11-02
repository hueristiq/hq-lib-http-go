package raw_test

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hqgohttpraw "github.com/hueristiq/hq-lib-http-go/raw"
)

func TestDo(t *testing.T) {
	t.Parallel()

	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	defer func() {
		_ = listener.Close()
	}()

	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}

		defer func() {
			_ = conn.Close()
		}()

		buffer := make([]byte, 1024)

		_, _ = conn.Read(buffer)

		_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 5\r\n\r\nhello")
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	response, err := hqgohttpraw.Do(ctx, listener.Addr().String(), []byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n"))
	require.NoError(t, err)

	defer func() {
		_ = response.Body.Close()
	}()

	assert.Equal(t, 200, response.StatusCode)

	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(body))
}

func TestDoRejectsEmptyInput(t *testing.T) {
	t.Parallel()

	_, err := hqgohttpraw.Do(context.Background(), "", []byte("GET / HTTP/1.1\r\n\r\n"))
	require.Error(t, err)

	_, err = hqgohttpraw.Do(context.Background(), "127.0.0.1:1", nil)
	require.Error(t, err)
}
