// Command faultproxy is an HTTP proxy that fails on purpose, for the
// acceptance harness's fault kit (the project's plan/prime-time-
// environment.md): set as HTTPS_PROXY, it makes every connection dockhand
// asks for stall, reset, or answer 502, or tunnels it through unchanged;
// stall-body tunnels it until the host has sent -after bytes, past TLS's
// handshake and a response's headers, then sends no more, so the stall
// falls in a body, not the handshake.
// It tunnels without decrypting, so it can cut a connection but never
// rewrite one.
//
//	faultproxy -mode stall|stall-body|reset|5xx|pass [-after 65536] [-listen 127.0.0.1:0]
//
// It prints the address it listens on, one line, then serves until it is
// stopped, logging each request's method and host, so a row can show
// which hosts dockhand asked.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"time"
)

func main() {
	mode := flag.String("mode", "stall", "what each connection meets: stall, stall-body, reset, 5xx, or pass")
	after := flag.Int64("after", 65536, "with stall-body, the bytes the host sends through before the stall")
	listen := flag.String("listen", "127.0.0.1:0", "the address to listen on")
	flag.Parse()
	switch *mode {
	case "stall", "stall-body", "reset", "5xx", "pass":
	default:
		fmt.Fprintf(os.Stderr, "faultproxy: -mode is stall, stall-body, reset, 5xx, or pass, not %q\n", *mode)
		os.Exit(2)
	}
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(listener.Addr().String())
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Fatal(err)
		}
		go serve(conn, *mode, *after)
	}
}

// serve answers one client connection as the mode says.
func serve(conn net.Conn, mode string, after int64) {
	defer conn.Close()
	request, err := http.ReadRequest(bufio.NewReader(conn))
	if err != nil {
		return
	}
	log.Printf("%s %s", request.Method, request.Host)
	switch mode {
	case "reset":
		if tcp, ok := conn.(*net.TCPConn); ok {
			_ = tcp.SetLinger(0)
		}
		return
	case "5xx":
		_, _ = io.WriteString(conn, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
		return
	case "stall":
		if request.Method == http.MethodConnect {
			_, _ = io.WriteString(conn, "HTTP/1.1 200 Connection established\r\n\r\n")
		}
		// Nothing more is ever sent: the client waits on data that
		// never comes, until it gives up or the proxy is stopped.
		_, _ = io.Copy(io.Discard, conn)
		return
	}
	if request.Method != http.MethodConnect {
		_, _ = io.WriteString(conn, "HTTP/1.1 501 Not Implemented\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
		return
	}
	upstream, err := net.DialTimeout("tcp", request.Host, 30*time.Second)
	if err != nil {
		_, _ = io.WriteString(conn, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
		return
	}
	defer upstream.Close()
	_, _ = io.WriteString(conn, "HTTP/1.1 200 Connection established\r\n\r\n")
	go func() { _, _ = io.Copy(upstream, conn) }()
	if mode != "stall-body" {
		_, _ = io.Copy(conn, upstream)
		return
	}
	// The host's first bytes go through, then nothing more, the
	// connection held open, and the stall said, so a row can show it
	// fell after the headers.
	sent, _ := io.CopyN(conn, upstream, after)
	if sent < after {
		return
	}
	log.Printf("stalled %s after %d bytes", request.Host, sent)
	_, _ = io.Copy(io.Discard, upstream)
}
