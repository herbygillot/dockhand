// Command faultproxy is an HTTP proxy that fails on purpose, for the
// acceptance harness's fault kit (the project's plan/prime-time-
// environment.md): set as HTTPS_PROXY, it makes every connection dockhand
// asks for stall, reset, or answer 502, or tunnels it through unchanged.
// It tunnels without decrypting, so it can cut a connection but never
// rewrite one.
//
//	faultproxy -mode stall|reset|5xx|pass [-listen 127.0.0.1:0]
//
// It prints the address it listens on, one line, then serves until it is
// stopped.
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
	mode := flag.String("mode", "stall", "what each connection meets: stall, reset, 5xx, or pass")
	listen := flag.String("listen", "127.0.0.1:0", "the address to listen on")
	flag.Parse()
	switch *mode {
	case "stall", "reset", "5xx", "pass":
	default:
		fmt.Fprintf(os.Stderr, "faultproxy: -mode is stall, reset, 5xx, or pass, not %q\n", *mode)
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
		go serve(conn, *mode)
	}
}

// serve answers one client connection as the mode says.
func serve(conn net.Conn, mode string) {
	defer conn.Close()
	request, err := http.ReadRequest(bufio.NewReader(conn))
	if err != nil {
		return
	}
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
	_, _ = io.Copy(conn, upstream)
}
