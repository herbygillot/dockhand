package fetch

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/jlaffaye/ftp"
)

// Scheme reports whether a master site's scheme is one the downloader speaks.
func Scheme(scheme string) bool { return scheme == "https" || scheme == "http" || scheme == "ftp" }

// OpenFTP retrieves one file by anonymous FTP and returns its body, bounded
// like an HTTP one. Credentials in the URL are refused: the ports tree's
// FTP master sites are public archives, and a secret would otherwise travel
// in the clear.
func OpenFTP(ctx context.Context, address string, limit int64) (io.ReadCloser, error) {
	parsed, err := url.Parse(address)
	if err != nil || parsed.Scheme != "ftp" || parsed.Host == "" || parsed.User != nil || parsed.Path == "" || strings.HasSuffix(parsed.Path, "/") || limit <= 0 {
		return nil, fmt.Errorf("fetch: unsupported FTP URL")
	}
	host := parsed.Host
	if _, _, err := net.SplitHostPort(host); err != nil {
		host = net.JoinHostPort(host, "21")
	}
	timeout := 30 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		timeout = min(timeout, time.Until(deadline))
	}
	conn, err := ftp.Dial(host, ftp.DialWithContext(ctx), ftp.DialWithTimeout(timeout))
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}
	if err := conn.Login("anonymous", "anonymous@"); err != nil {
		conn.Quit()
		return nil, fmt.Errorf("fetch: FTP login: %w", err)
	}
	path, err := url.PathUnescape(parsed.Path)
	if err != nil {
		conn.Quit()
		return nil, fmt.Errorf("fetch: unsupported FTP URL")
	}
	response, err := conn.Retr(path)
	if err != nil {
		conn.Quit()
		return nil, fmt.Errorf("fetch: FTP retrieve %s: %w", path, err)
	}
	return &ftpBody{boundedBody: boundedBody{ReadCloser: response, remaining: limit}, conn: conn}, nil
}

// ftpBody closes the transfer and then the control connection.
type ftpBody struct {
	boundedBody
	conn *ftp.ServerConn
}

func (b *ftpBody) Close() error {
	err := b.boundedBody.Close()
	b.conn.Quit()
	return err
}
