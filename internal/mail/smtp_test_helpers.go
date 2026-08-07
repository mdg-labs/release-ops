package mail

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/smtp"
	"strings"
	"sync"
	"testing"
	"time"
)

type pipeConn struct{}

func (pipeConn) Read([]byte) (int, error)  { return 0, io.EOF }
func (pipeConn) Write([]byte) (int, error) { return len([]byte{}), nil }
func (pipeConn) Close() error              { return nil }
func (pipeConn) LocalAddr() net.Addr       { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1} }
func (pipeConn) RemoteAddr() net.Addr      { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 2} }
func (pipeConn) SetDeadline(time.Time) error      { return nil }
func (pipeConn) SetReadDeadline(time.Time) error  { return nil }
func (pipeConn) SetWriteDeadline(time.Time) error { return nil }

type mockSMTPClient struct {
	onMail func(string) error
	onRcpt func(string) error
	onData func([]byte) error
}

func (m *mockSMTPClient) Extension(ext string) (bool, string) {
	if ext == "STARTTLS" {
		return true, ""
	}
	return false, ""
}

func (m *mockSMTPClient) StartTLS(*tls.Config) error { return nil }

func (m *mockSMTPClient) Auth(smtp.Auth) error { return nil }

func (m *mockSMTPClient) Mail(from string) error {
	if m.onMail != nil {
		return m.onMail(from)
	}
	return nil
}

func (m *mockSMTPClient) Rcpt(to string) error {
	if m.onRcpt != nil {
		return m.onRcpt(to)
	}
	return nil
}

func (m *mockSMTPClient) Data() (smtpWriter, error) {
	return &mockSMTPWriter{onClose: m.onData}, nil
}

func (m *mockSMTPClient) Quit() error  { return nil }
func (m *mockSMTPClient) Close() error { return nil }

type mockSMTPWriter struct {
	buf     bytes.Buffer
	onClose func([]byte) error
}

func (w *mockSMTPWriter) Write(p []byte) (int, error) {
	return w.buf.Write(p)
}

func (w *mockSMTPWriter) Close() error {
	if w.onClose != nil {
		return w.onClose(w.buf.Bytes())
	}
	return nil
}

type testSMTPServer struct {
	Port    int
	ln      net.Listener
	mu      sync.Mutex
	message string
	ready   chan struct{}
}

func startTestSMTPServer(t *testing.T) *testSMTPServer {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	port := ln.Addr().(*net.TCPAddr).Port
	server := &testSMTPServer{
		Port:  port,
		ln:    ln,
		ready: make(chan struct{}, 1),
	}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go server.handleConn(conn)
		}
	}()

	return server
}

func (s *testSMTPServer) Close() {
	_ = s.ln.Close()
}

func (s *testSMTPServer) WaitForMessage() string {
	select {
	case <-s.ready:
	case <-time.After(2 * time.Second):
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.message
}

func (s *testSMTPServer) handleConn(conn net.Conn) {
	defer func() { _ = conn.Close() }()

	reader := bufio.NewReader(conn)
	_, _ = conn.Write([]byte("220 test-smtp ready\r\n"))

	var data bytes.Buffer
	inData := false

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		trimmed := strings.TrimSpace(line)

		if inData {
			if trimmed == "." {
				s.mu.Lock()
				s.message = data.String()
				s.mu.Unlock()
				select {
				case s.ready <- struct{}{}:
				default:
				}
				_, _ = conn.Write([]byte("250 ok\r\n"))
				inData = false
				data.Reset()
				continue
			}
			data.WriteString(line)
			continue
		}

		switch {
		case strings.HasPrefix(trimmed, "EHLO"), strings.HasPrefix(trimmed, "HELO"):
			_, _ = conn.Write([]byte("250-test-smtp\r\n250 AUTH PLAIN LOGIN\r\n"))
		case strings.HasPrefix(trimmed, "AUTH"):
			_, _ = conn.Write([]byte("235 auth ok\r\n"))
		case strings.HasPrefix(trimmed, "MAIL FROM:"):
			_, _ = conn.Write([]byte("250 ok\r\n"))
		case strings.HasPrefix(trimmed, "RCPT TO:"):
			_, _ = conn.Write([]byte("250 ok\r\n"))
		case trimmed == "DATA":
			inData = true
			_, _ = conn.Write([]byte("354 start mail input\r\n"))
		case trimmed == "QUIT":
			_, _ = conn.Write([]byte("221 bye\r\n"))
			return
		default:
			_, _ = fmt.Fprintf(conn, "250 %s\r\n", trimmed)
		}
	}
}
