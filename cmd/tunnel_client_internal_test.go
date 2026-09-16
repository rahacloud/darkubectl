package cmd

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	chserver "github.com/jpillora/chisel/server"
)

// The client is linked in rather than executed, so this proves the real thing:
// a chisel server, a TCP service behind it, and bytes making the round trip
// through runChiselClient's forward. Without this the embedding is only checked
// by the code compiling, which would not catch a wrong config field or a client
// that returns before its listener is up.
func TestRunChiselClientForwardsThroughARealServer(t *testing.T) {
	t.Parallel()

	const auth = "tunnel:integration-secret"
	echo := startEchoServer(t)
	server := startChiselServer(t, auth)

	local := freePort(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- runChiselClient(ctx, "http://"+server, auth,
			[]string{fmt.Sprintf("%d:%s", local, echo)})
	}()

	got := dialUntilEcho(t, local, "hello through the tunnel")
	if got != "hello through the tunnel" {
		t.Fatalf("echo returned %q", got)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Error("runChiselClient did not return after its context was cancelled")
	}
}

// A wrong credential must fail, or the credential is decorative.
func TestRunChiselClientRejectsABadCredential(t *testing.T) {
	t.Parallel()

	echo := startEchoServer(t)
	server := startChiselServer(t, "tunnel:right")

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	err := runChiselClient(ctx, "http://"+server, "tunnel:wrong",
		[]string{fmt.Sprintf("%d:%s", freePort(t), echo)})
	if err == nil {
		t.Fatal("a wrong credential connected")
	}
}

func startChiselServer(t *testing.T, auth string) string {
	t.Helper()

	s, err := chserver.NewServer(&chserver.Config{Auth: auth, KeepAlive: chiselKeepalive})
	if err != nil {
		t.Fatalf("chisel server: %v", err)
	}
	port := freePort(t)
	if err := s.StartContext(t.Context(), "127.0.0.1", strconv.Itoa(port)); err != nil {
		t.Fatalf("starting chisel server: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return fmt.Sprintf("127.0.0.1:%d", port)
}

// startEchoServer stands in for the in-cluster service a real forward targets.
func startEchoServer(t *testing.T) string {
	t.Helper()

	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("echo listener: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				_, _ = io.Copy(conn, conn)
			}()
		}
	}()
	return ln.Addr().String()
}

// dialUntilEcho retries: Start returns once the listener is bound, but the
// websocket to the server may still be coming up behind it.
func dialUntilEcho(t *testing.T, port int, msg string) string {
	t.Helper()

	deadline := time.Now().Add(20 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		dialer := net.Dialer{Timeout: time.Second}
		conn, err := dialer.DialContext(t.Context(), "tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			last = err
			time.Sleep(200 * time.Millisecond)
			continue
		}
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		if _, err := io.WriteString(conn, msg); err != nil {
			last = err
			_ = conn.Close()
			time.Sleep(200 * time.Millisecond)
			continue
		}
		buf := make([]byte, len(msg))
		_, err = io.ReadFull(conn, buf)
		_ = conn.Close()
		if err != nil {
			last = err
			time.Sleep(200 * time.Millisecond)
			continue
		}
		return strings.TrimSpace(string(buf))
	}
	t.Fatalf("nothing came back through the forward: %v", last)
	return ""
}

func freePort(t *testing.T) int {
	t.Helper()

	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserving a port: %v", err)
	}
	defer func() { _ = ln.Close() }()
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener address %v is not TCP", ln.Addr())
	}
	return addr.Port
}
