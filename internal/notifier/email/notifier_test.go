package email

import (
	"bufio"
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

// fakeSMTP is a minimal in-process SMTP server capturing one message.
type fakeSMTP struct {
	ln       net.Listener
	mu       sync.Mutex
	from     string
	rcpts    []string
	data     string
	authLine string
}

func newFakeSMTP(t *testing.T) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &fakeSMTP{ln: ln}
	go s.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return s
}

func (s *fakeSMTP) addrPort() (string, int) {
	addr := s.ln.Addr().(*net.TCPAddr)
	return addr.IP.String(), addr.Port
}

func (s *fakeSMTP) serve() {
	conn, err := s.ln.Accept()
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	say := func(line string) {
		_, _ = w.WriteString(line + "\r\n")
		_ = w.Flush()
	}
	say("220 fake.local ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		cmd := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			say("250-fake.local")
			say("250 AUTH PLAIN")
		case strings.HasPrefix(cmd, "AUTH"):
			s.mu.Lock()
			s.authLine = line
			s.mu.Unlock()
			say("235 ok")
		case strings.HasPrefix(cmd, "MAIL FROM:"):
			s.mu.Lock()
			s.from = line
			s.mu.Unlock()
			say("250 ok")
		case strings.HasPrefix(cmd, "RCPT TO:"):
			s.mu.Lock()
			s.rcpts = append(s.rcpts, line)
			s.mu.Unlock()
			say("250 ok")
		case cmd == "DATA":
			say("354 go ahead")
			var b strings.Builder
			for {
				dl, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimRight(dl, "\r\n") == "." {
					break
				}
				b.WriteString(dl)
			}
			s.mu.Lock()
			s.data = b.String()
			s.mu.Unlock()
			say("250 queued")
		case cmd == "QUIT":
			say("221 bye")
			return
		default:
			say("250 ok")
		}
	}
}

func (s *fakeSMTP) snapshot() (from string, rcpts []string, data, auth string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.from, append([]string(nil), s.rcpts...), s.data, s.authLine
}

func testEvent() domain.Event {
	return domain.Event{
		RunName:      "build-run-1",
		PipelineName: "build-and-test",
		Namespace:    "ci",
		State:        domain.StateFailure,
		CommitSHA:    "0123456789abcdef",
		Description:  "unit tests failed",
		TargetURL:    "https://tekton.example.com/run/1",
		Results:      []domain.Result{{Name: "coverage", Value: "81%"}},
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}

func TestHandle_SendsMessage(t *testing.T) {
	srv := newFakeSMTP(t)
	host, port := srv.addrPort()

	n, err := New(Config{
		Name: "default", Host: host, Port: port, Encryption: EncryptionNone,
		From: "ci@example.com", To: []string{"team@example.com", "lead@example.com"},
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := n.Handle(context.Background(), testEvent()); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	waitFor(t, func() bool { _, _, data, _ := srv.snapshot(); return data != "" })
	from, rcpts, data, _ := srv.snapshot()

	if !strings.Contains(from, "ci@example.com") {
		t.Errorf("MAIL FROM = %q", from)
	}
	if len(rcpts) != 2 {
		t.Errorf("expected 2 recipients, got %v", rcpts)
	}
	for _, want := range []string{
		"Subject: [tekton] build-and-test", "failure",
		"Run:       build-run-1", "Commit:    01234567",
		"View logs: https://tekton.example.com/run/1", "- coverage: 81%",
	} {
		if !strings.Contains(data, want) {
			t.Errorf("message missing %q in:\n%s", want, data)
		}
	}
	if !strings.Contains(data, "Content-Type: text/plain") {
		t.Error("expected text/plain content type")
	}
}

func TestHandle_AuthAndCustomTemplates(t *testing.T) {
	srv := newFakeSMTP(t)
	host, port := srv.addrPort()

	n, err := New(Config{
		Name: "auth", Host: host, Port: port, Encryption: EncryptionNone,
		Username: "user", Password: "pass",
		From: "ci@example.com", To: []string{"team@example.com"},
		Subject:  "{{ .RunName }} is {{ .State }}",
		Template: "state={{ .State }}",
		HTML:     true,
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := n.Handle(context.Background(), testEvent()); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	waitFor(t, func() bool { _, _, data, _ := srv.snapshot(); return data != "" })
	_, _, data, auth := srv.snapshot()

	if auth == "" {
		t.Error("expected AUTH command")
	}
	if !strings.Contains(data, "Subject: build-run-1 is failure") {
		t.Errorf("custom subject missing:\n%s", data)
	}
	if !strings.Contains(data, "state=failure") {
		t.Errorf("custom body missing:\n%s", data)
	}
	if !strings.Contains(data, "Content-Type: text/html") {
		t.Error("expected text/html content type")
	}
}

func TestHandle_SubjectHeaderInjectionStripped(t *testing.T) {
	srv := newFakeSMTP(t)
	host, port := srv.addrPort()

	n, err := New(Config{
		Name: "inj", Host: host, Port: port, Encryption: EncryptionNone,
		From: "ci@example.com", To: []string{"team@example.com"},
		Subject: "{{ .Description }}",
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	e := testEvent()
	e.Description = "evil\r\nBcc: attacker@example.com"
	if err := n.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	waitFor(t, func() bool { _, _, data, _ := srv.snapshot(); return data != "" })
	_, _, data, _ := srv.snapshot()

	// the injected text must stay inside the Subject line — no header line
	// (anything before the blank separator) may become a new Bcc header
	headers, _, _ := strings.Cut(data, "\r\n\r\n")
	for _, line := range strings.Split(headers, "\r\n") {
		if strings.HasPrefix(line, "Bcc:") {
			t.Errorf("header injection not stripped:\n%s", data)
		}
	}
	if !strings.Contains(data, "Subject: evil") {
		t.Errorf("expected flattened subject, got:\n%s", data)
	}
}

func TestNew_Validation(t *testing.T) {
	log := zap.NewNop()
	base := Config{Host: "smtp.example.com", From: "a@b.c", To: []string{"d@e.f"}}

	if _, err := New(Config{From: base.From, To: base.To}, log); err == nil {
		t.Error("expected error for missing host")
	}
	if _, err := New(Config{Host: base.Host, To: base.To}, log); err == nil {
		t.Error("expected error for missing from")
	}
	if _, err := New(Config{Host: base.Host, From: base.From}, log); err == nil {
		t.Error("expected error for missing recipients")
	}
	bad := base
	bad.Encryption = "ssl3"
	if _, err := New(bad, log); err == nil {
		t.Error("expected error for invalid encryption")
	}
	bad = base
	bad.Subject = "{{ .Oops"
	if _, err := New(bad, log); err == nil {
		t.Error("expected error for bad subject template")
	}
	if _, err := New(base, log); err != nil {
		t.Errorf("valid config rejected: %v", err)
	}
}

func TestHandle_ContextCancellation(t *testing.T) {
	// Listener that accepts but never speaks SMTP — Handle must time out
	// via the context, not hang.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			defer func() { _ = conn.Close() }()
			time.Sleep(5 * time.Second)
		}
	}()

	addr := ln.Addr().(*net.TCPAddr)
	n, err := New(Config{
		Name: "hang", Host: addr.IP.String(), Port: addr.Port, Encryption: EncryptionNone,
		From: "a@b.c", To: []string{"d@e.f"},
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := n.Handle(ctx, testEvent()); err == nil {
		t.Fatal("expected error from canceled context")
	}
	if time.Since(start) > 2*time.Second {
		t.Error("Handle did not honor context deadline")
	}
}
