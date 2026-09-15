package provision

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

func TestSSHHandshakeIsBounded(t *testing.T) {
	for _, cancelContext := range []bool{false, true} {
		t.Run(map[bool]string{false: "timeout", true: "cancellation"}[cancelContext], func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			defer listener.Close()
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				_, _ = io.Copy(io.Discard, conn)
			}()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			timeout := 30 * time.Millisecond
			if cancelContext {
				timeout = time.Second
				time.AfterFunc(30*time.Millisecond, cancel)
			}
			_, err = dialSSH(ctx, listener.Addr().String(), &ssh.ClientConfig{User: "fixture", HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: timeout})
			require.Error(t, err)
			if cancelContext {
				require.ErrorIs(t, err, context.Canceled)
			}
			select {
			case <-finished:
			case <-time.After(time.Second):
				t.Fatal("handshake connection leaked")
			}
		})
	}
}
