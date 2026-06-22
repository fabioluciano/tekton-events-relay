package email

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

const (
	fromAddr = "ci@example.com"
	toAddr   = "team@example.com"
)

// capturedMsg holds one SMTP transaction observed by the fake server.
type capturedMsg struct {
	from     string
	rcpts    []string
	data     string
	authLine string
}

// fakeSMTP is a minimal in-process SMTP server capturing every message it
// receives across multiple connections.
type fakeSMTP struct {
	ln   net.Listener
	mu   sync.Mutex
	msgs []capturedMsg
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
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *fakeSMTP) handle(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	say := func(line string) {
		_, _ = w.WriteString(line + "\r\n")
		_ = w.Flush()
	}
	say("220 fake.local ESMTP")
	var cur capturedMsg
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
			say("250 AUTH PLAIN LOGIN XOAUTH2")
		case strings.HasPrefix(cmd, "AUTH"):
			cur.authLine = line
			say("235 ok")
		case strings.HasPrefix(cmd, "MAIL FROM:"):
			cur.from = line
			say("250 ok")
		case strings.HasPrefix(cmd, "RCPT TO:"):
			cur.rcpts = append(cur.rcpts, line)
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
			cur.data = b.String()
			s.mu.Lock()
			s.msgs = append(s.msgs, cur)
			s.mu.Unlock()
			cur = capturedMsg{}
			say("250 queued")
		case cmd == "RSET":
			cur = capturedMsg{}
			say("250 ok")
		case cmd == "QUIT":
			say("221 bye")
			return
		default:
			say("250 ok")
		}
	}
}

func (s *fakeSMTP) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.msgs)
}

func (s *fakeSMTP) at(i int) capturedMsg {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.msgs[i]
	m.rcpts = append([]string(nil), m.rcpts...)
	return m
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

func waitForCount(t *testing.T, s *fakeSMTP, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s.count() >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected %d messages, got %d", want, s.count())
}

func headerSection(data string) string {
	headers, _, _ := strings.Cut(data, "\r\n\r\n")
	return headers
}

func TestHandle_SendsMessage(t *testing.T) {
	srv := newFakeSMTP(t)
	host, port := srv.addrPort()

	n, err := New(Config{
		Name: "default", Host: host, Port: port, Encryption: EncryptionNone,
		From: fromAddr, To: []string{toAddr, "lead@example.com"},
		Subject: "[tekton] {{ if .PipelineName }}{{ .PipelineName }}{{ else }}{{ .RunName }}{{ end }} — {{ .State }}",
		Template: `Pipeline {{ .State }}: {{ if .PipelineName }}{{ .PipelineName }}{{ else }}{{ .RunName }}{{ end }}

Run:       {{ .RunName }}
Namespace: {{ .Namespace }}
{{- if .CommitSHA }}
Commit:    {{ printf "%.8s" .CommitSHA }}
{{- end }}
{{- if .Description }}

{{ .Description }}
{{- end }}
{{- if .Results }}

Results:
{{- range .Results }}
- {{ .Name }}: {{ .Value }}
{{- end }}
{{- end }}
{{- if .TargetURL }}

View logs: {{ .TargetURL }}
{{- end }}`,
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := n.Handle(context.Background(), testEvent()); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	waitForCount(t, srv, 1)
	m := srv.at(0)

	if !strings.Contains(m.from, fromAddr) {
		t.Errorf("MAIL FROM = %q", m.from)
	}
	if len(m.rcpts) != 2 {
		t.Errorf("expected 2 recipients, got %v", m.rcpts)
	}
	for _, want := range []string{
		"failure", "Run:       build-run-1", "Commit:    01234567",
		"View logs: https://tekton.example.com/run/1", "- coverage: 81%",
	} {
		if !strings.Contains(m.data, want) {
			t.Errorf("message missing %q in:\n%s", want, m.data)
		}
	}
	if !strings.Contains(m.data, "build-and-test") {
		t.Errorf("subject missing pipeline name:\n%s", headerSection(m.data))
	}
	if !strings.Contains(m.data, "text/plain") {
		t.Error("expected text/plain content type")
	}
}

func TestHandle_AuthAndCustomTemplates(t *testing.T) {
	srv := newFakeSMTP(t)
	host, port := srv.addrPort()

	n, err := New(Config{
		Name: "auth", Host: host, Port: port, Encryption: EncryptionNone,
		Username: "user", Password: "pass",
		From: fromAddr, To: []string{toAddr},
		Subject:  "{{ .RunName }} is {{ .State }}",
		Template: "<b>state is {{ .State }}</b>",
		HTML:     true,
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := n.Handle(context.Background(), testEvent()); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	waitForCount(t, srv, 1)
	m := srv.at(0)

	if m.authLine == "" {
		t.Error("expected AUTH command")
	}
	if !strings.Contains(m.data, "build-run-1 is failure") {
		t.Errorf("custom subject missing:\n%s", m.data)
	}
	if !strings.Contains(m.data, "state is failure") {
		t.Errorf("custom body missing:\n%s", m.data)
	}
	if !strings.Contains(m.data, "text/html") {
		t.Error("expected text/html content type")
	}
}

func TestHandle_CcBcc(t *testing.T) {
	srv := newFakeSMTP(t)
	host, port := srv.addrPort()

	n, err := New(Config{
		Name: "ccbcc", Host: host, Port: port, Encryption: EncryptionNone,
		From:     fromAddr,
		To:       []string{toAddr},
		Cc:       []string{"cc@example.com"},
		Bcc:      []string{"audit@example.com"},
		Subject:  "{{ .State }}",
		Template: "body {{ .RunName }}",
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := n.Handle(context.Background(), testEvent()); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	waitForCount(t, srv, 1)
	m := srv.at(0)

	// All three appear in the envelope (RCPT TO).
	joined := strings.Join(m.rcpts, " ")
	for _, want := range []string{toAddr, "cc@example.com", "audit@example.com"} {
		if !strings.Contains(joined, want) {
			t.Errorf("envelope missing %q in %v", want, m.rcpts)
		}
	}

	headers := headerSection(m.data)
	if !strings.Contains(headers, "cc@example.com") {
		t.Errorf("Cc not in visible headers:\n%s", headers)
	}
	// Bcc must never leak into the rendered headers.
	if strings.Contains(headers, "audit@example.com") {
		t.Errorf("Bcc leaked into headers:\n%s", headers)
	}
}

func TestHandle_Threading(t *testing.T) {
	srv := newFakeSMTP(t)
	host, port := srv.addrPort()

	n, err := New(Config{
		Name: "thread", Host: host, Port: port, Encryption: EncryptionNone,
		From: fromAddr, To: []string{toAddr},
		Subject:  "{{ .State }}",
		Template: "body {{ .RunName }}",
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	e := testEvent()
	e.RunID = "uid-threading-1"

	// First send establishes the root Message-ID; no threading headers.
	if err := n.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle #1: %v", err)
	}
	waitForCount(t, srv, 1)
	first := headerSection(srv.at(0).data)
	if strings.Contains(first, "In-Reply-To:") {
		t.Errorf("first message must not carry In-Reply-To:\n%s", first)
	}

	rootID := extractHeader(first, "Message-ID:")
	if rootID == "" {
		t.Fatalf("first message missing Message-ID:\n%s", first)
	}

	// Second send for the same run threads under the root.
	e.State = domain.StateSuccess
	if err := n.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle #2: %v", err)
	}
	waitForCount(t, srv, 2)
	second := headerSection(srv.at(1).data)

	inReplyTo := extractHeader(second, "In-Reply-To:")
	if inReplyTo == "" {
		t.Errorf("second message missing In-Reply-To:\n%s", second)
	}
	if inReplyTo != rootID {
		t.Errorf("In-Reply-To = %q, want root %q", inReplyTo, rootID)
	}
	if refs := extractHeader(second, "References:"); refs != rootID {
		t.Errorf("References = %q, want root %q", refs, rootID)
	}
}

func TestHandle_XOAuth2RotatingToken(t *testing.T) {
	srv := newFakeSMTP(t)
	host, port := srv.addrPort()

	rot := &rotatingToken{}
	n, err := New(Config{
		Name: "oauth", Host: host, Port: port, Encryption: EncryptionNone,
		Username: "user@example.com", XOAuth2: true, Token: rot,
		From: fromAddr, To: []string{toAddr},
		Subject:  "{{ .State }}",
		Template: "body {{ .RunName }}",
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	e := testEvent()
	e.RunID = "uid-oauth-1"
	if err := n.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle #1: %v", err)
	}
	e.State = domain.StateSuccess
	if err := n.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle #2: %v", err)
	}
	waitForCount(t, srv, 2)

	tok1 := xoauth2Token(t, srv.at(0).authLine)
	tok2 := xoauth2Token(t, srv.at(1).authLine)
	if tok1 == "" || tok2 == "" {
		t.Fatalf("expected XOAUTH2 tokens, got %q and %q", tok1, tok2)
	}
	if tok1 == tok2 {
		t.Errorf("token not refreshed per request: both %q", tok1)
	}
}

func TestHandle_InvalidRecipientRejected(t *testing.T) {
	n, err := New(Config{
		Name: "inv", Host: "smtp.example.com", Port: 2525, Encryption: EncryptionNone,
		From: fromAddr, To: []string{"not-an-email"},
		Subject:  "{{ .State }}",
		Template: "body {{ .RunName }}",
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := n.Handle(context.Background(), testEvent()); err == nil {
		t.Fatal("expected error for invalid recipient address")
	}
}

func TestHandle_SubjectHeaderInjectionStripped(t *testing.T) {
	srv := newFakeSMTP(t)
	host, port := srv.addrPort()

	n, err := New(Config{
		Name: "inj", Host: host, Port: port, Encryption: EncryptionNone,
		From: fromAddr, To: []string{toAddr},
		Subject:  "{{ .Description }}",
		Template: "Body {{ .RunName }}",
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	e := testEvent()
	e.Description = "evil\r\nBcc: attacker@example.com"
	if err := n.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	waitForCount(t, srv, 1)
	headers := headerSection(srv.at(0).data)
	for _, line := range strings.Split(headers, "\r\n") {
		if strings.HasPrefix(line, "Bcc:") {
			t.Errorf("header injection not stripped:\n%s", headers)
		}
	}
}

func TestNew_Validation(t *testing.T) {
	log := zap.NewNop()
	base := Config{
		Host:     "smtp.example.com",
		From:     "a@b.c",
		To:       []string{"d@e.f"},
		Subject:  "Test {{ .State }}",
		Template: "Body {{ .RunName }}",
	}

	if _, err := New(Config{From: base.From, To: base.To, Subject: base.Subject, Template: base.Template}, log); err == nil {
		t.Error("expected error for missing host")
	}
	if _, err := New(Config{Host: base.Host, To: base.To, Subject: base.Subject, Template: base.Template}, log); err == nil {
		t.Error("expected error for missing from")
	}
	if _, err := New(Config{Host: base.Host, From: base.From, Subject: base.Subject, Template: base.Template}, log); err == nil {
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
	bad = base
	bad.Subject = ""
	if _, err := New(bad, log); err == nil {
		t.Error("expected error for missing subject template")
	}
	bad = base
	bad.Template = ""
	if _, err := New(bad, log); err == nil {
		t.Error("expected error for missing body template")
	}
	bad = base
	bad.XOAuth2 = true
	bad.Username = "user"
	if _, err := New(bad, log); err == nil {
		t.Error("expected error for xoauth2 without token source")
	}
	bad = base
	bad.XOAuth2 = true
	bad.Token = &rotatingToken{}
	if _, err := New(bad, log); err == nil {
		t.Error("expected error for xoauth2 without username")
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
		Subject:  "Test {{ .State }}",
		Template: "Body {{ .RunName }}",
		Timeout:  200 * time.Millisecond,
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

func TestNew_FileTemplate(t *testing.T) {
	log := zap.NewNop()
	tmpfile, err := os.CreateTemp("", "email-test-*.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(tmpfile.Name()) }()

	if _, err := tmpfile.Write([]byte("File template {{ .State }}")); err != nil {
		t.Fatal(err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatal(err)
	}

	cfg := Config{
		Host:     "smtp.example.com",
		From:     "test@example.com",
		To:       []string{"user@example.com"},
		Subject:  "Test {{ .State }}",
		Template: tmpfile.Name(),
	}

	n, err := New(cfg, log)
	if err != nil {
		t.Fatalf("New with file template: %v", err)
	}
	if n == nil {
		t.Error("expected notifier, got nil")
	}
}

// rotatingToken is a TokenRefresher that returns a fresh value on every call,
// proving the handler resolves the XOAUTH2 token per request.
type rotatingToken struct {
	mu sync.Mutex
	n  int
}

func (r *rotatingToken) Token(_ context.Context) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.n++
	return fmt.Sprintf("access-token-%d", r.n), nil
}

// extractHeader returns the unfolded value of the named header (prefix includes
// the trailing colon, e.g. "Message-ID:").
func extractHeader(headers, prefix string) string {
	for _, line := range strings.Split(headers, "\r\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

// xoauth2Token decodes the base64 SASL payload of an "AUTH XOAUTH2 <b64>" line
// and returns the bearer access token.
func xoauth2Token(t *testing.T, authLine string) string {
	t.Helper()
	fields := strings.Fields(authLine)
	if len(fields) < 3 || !strings.EqualFold(fields[1], "XOAUTH2") {
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(fields[2])
	if err != nil {
		t.Fatalf("decode xoauth2 payload: %v", err)
	}
	// Format: user=<user>\x01auth=Bearer <token>\x01\x01
	for _, part := range strings.Split(string(raw), "\x01") {
		if strings.HasPrefix(part, "auth=Bearer ") {
			return strings.TrimPrefix(part, "auth=Bearer ")
		}
	}
	return ""
}
