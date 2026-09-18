package fetch_test

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/fetch"
	"github.com/stretchr/testify/require"
)

// ftpServer is the least an anonymous FTP archive answers: login, a
// passive data connection, and one file, so the client's transfer and its
// bound are exercised without a network.
func ftpServer(t *testing.T, files map[string]string) string {
	t.Helper()
	control, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { control.Close() })
	go func() {
		for {
			conn, err := control.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				reply := func(format string, args ...any) { fmt.Fprintf(conn, format+"\r\n", args...) }
				reply("220 fixture")
				var data net.Listener
				scanner := bufio.NewScanner(conn)
				for scanner.Scan() {
					command, argument, _ := strings.Cut(scanner.Text(), " ")
					switch strings.ToUpper(command) {
					case "USER":
						reply("331 password")
					case "PASS":
						reply("230 anonymous")
					case "FEAT":
						reply("211-features\r\n EPSV\r\n211 end")
					case "TYPE":
						reply("200 type")
					case "EPSV":
						data, err = net.Listen("tcp", "127.0.0.1:0")
						if err != nil {
							reply("425 no data connection")
							continue
						}
						reply("229 Entering Extended Passive Mode (|||%d|)", data.Addr().(*net.TCPAddr).Port)
					case "PASV":
						reply("502 use EPSV")
					case "RETR":
						body, ok := files[argument]
						if !ok || data == nil {
							reply("550 no such file")
							continue
						}
						reply("150 sending")
						dataConn, err := data.Accept()
						if err == nil {
							io.WriteString(dataConn, body)
							dataConn.Close()
						}
						data.Close()
						data = nil
						reply("226 done")
					case "QUIT":
						reply("221 bye")
						return
					default:
						reply("500 unknown")
					}
				}
			}()
		}
	}()
	return "ftp://" + control.Addr().String()
}

func TestOpenFTPRetrievesAnonymouslyWithinTheBound(t *testing.T) {
	t.Parallel()
	address := ftpServer(t, map[string]string{"/pub/misc/ndiff-2.00.tar.gz": "archive bytes"})
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	body, err := fetch.OpenFTP(ctx, address+"/pub/misc/ndiff-2.00.tar.gz", 1<<20)
	require.NoError(t, err)
	content, err := io.ReadAll(body)
	require.NoError(t, err)
	require.NoError(t, body.Close())
	require.Equal(t, "archive bytes", string(content))

	_, err = fetch.OpenFTP(ctx, address+"/pub/misc/missing.tar.gz", 1<<20)
	require.Error(t, err, "a missing file is an error, not an empty archive")
	body, err = fetch.OpenFTP(ctx, address+"/pub/misc/ndiff-2.00.tar.gz", 4)
	require.NoError(t, err)
	_, err = io.ReadAll(body)
	require.Error(t, err, "the size bound holds for FTP as for HTTP")
	body.Close()
	for _, bad := range []string{"ftp://user:secret@" + strings.TrimPrefix(address, "ftp://") + "/pub/x", address + "/pub/misc/", "http://example.invalid/x"} {
		_, err = fetch.OpenFTP(ctx, bad, 1<<20)
		require.Error(t, err, bad)
	}
}
