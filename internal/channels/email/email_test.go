package email

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/approval"
	"or3-intern/internal/bus"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
)

func openEmailTestDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "email.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func baseConfig() config.EmailChannelConfig {
	return config.EmailChannelConfig{
		Enabled:             true,
		ConsentGranted:      true,
		OpenAccess:          true,
		PollIntervalSeconds: 1,
		MaxBodyChars:        200,
		IMAPHost:            "imap.example.com",
		IMAPUsername:        "imap-user@example.com",
		IMAPPassword:        "imap-pass",
		SMTPHost:            "smtp.example.com",
		SMTPUsername:        "smtp-user@example.com",
		SMTPPassword:        "smtp-pass",
	}
}

func TestChannel_AllowedSenderSupportsPairingPolicy(t *testing.T) {
	broker := &approval.Broker{DB: openEmailTestDB(t)}
	if _, _, err := broker.RotateDeviceToken(context.Background(), "email:alice@example.com", approval.RoleOperator, "Email User", map[string]any{"channel": "email", "identity": "alice@example.com"}); err != nil {
		t.Fatalf("RotateDeviceToken: %v", err)
	}
	channel := &Channel{
		Config:         config.EmailChannelConfig{InboundPolicy: config.InboundPolicyPairing},
		ApprovalBroker: broker,
	}
	if !channel.allowedSender(context.Background(), "alice@example.com") {
		t.Fatal("expected paired email sender to be allowed")
	}
	if channel.allowedSender(context.Background(), "bob@example.com") {
		t.Fatal("expected unknown email sender to be denied")
	}
}

func TestParseRawEmail_PrefersPlainTextAndDecodesHeaders(t *testing.T) {
	raw := strings.Join([]string{
		"From: Sender <sender@example.com>",
		"Subject: =?UTF-8?Q?Status_=E2=9C=85?=",
		"Date: Sun, 08 Mar 2026 14:15:16 +0000",
		"Message-ID: <mid-1@example.com>",
		"MIME-Version: 1.0",
		"Content-Type: multipart/alternative; boundary=abc",
		"",
		"--abc",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		"plain body",
		"--abc",
		"Content-Type: text/html; charset=UTF-8",
		"",
		"<p>html body</p>",
		"--abc--",
	}, "\r\n")

	parsed, err := parseRawEmail([]byte(raw), 200)
	if err != nil {
		t.Fatalf("parseRawEmail: %v", err)
	}
	if parsed.From != "sender@example.com" {
		t.Fatalf("unexpected sender: %q", parsed.From)
	}
	if parsed.Subject != "Status ✅" {
		t.Fatalf("unexpected subject: %q", parsed.Subject)
	}
	if parsed.MessageID != "<mid-1@example.com>" {
		t.Fatalf("unexpected message id: %q", parsed.MessageID)
	}
	if parsed.Body != "plain body" {
		t.Fatalf("expected plain body preference, got %q", parsed.Body)
	}
}

func TestParseRawEmail_UsesHTMLFallback(t *testing.T) {
	raw := strings.Join([]string{
		"From: sender@example.com",
		"Subject: HTML only",
		"Content-Type: text/html; charset=UTF-8",
		"",
		"<div>Hello<br />WORLD</div>",
	}, "\r\n")

	parsed, err := parseRawEmail([]byte(raw), 200)
	if err != nil {
		t.Fatalf("parseRawEmail: %v", err)
	}
	if !strings.Contains(parsed.Body, "Hello") || !strings.Contains(parsed.Body, "WORLD") {
		t.Fatalf("expected readable html fallback body, got %q", parsed.Body)
	}
}

func TestChannel_StartPublishesInboundEmailEvent(t *testing.T) {
	config := baseConfig()
	channel := &Channel{
		Config: config,
		FetchMessages: func(ctx context.Context) ([]InboundMessage, error) {
			return []InboundMessage{{
				UID:       "101",
				From:      "Alice <alice@example.com>",
				Subject:   "Project update",
				MessageID: "<msg-101@example.com>",
				Date:      time.Date(2026, time.March, 8, 12, 0, 0, 0, time.UTC),
				Body:      "hello from email",
			}}, nil
		},
	}
	eventBus := bus.New(1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := channel.Start(ctx, eventBus); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		if err := channel.Stop(context.Background()); err != nil {
			t.Fatalf("Stop: %v", err)
		}
	}()

	select {
	case event := <-eventBus.Channel():
		if event.Channel != "email" || event.SessionKey != "email:alice@example.com" || event.From != "alice@example.com" {
			t.Fatalf("unexpected event envelope: %#v", event)
		}
		if !strings.Contains(event.Message, "Subject: Project update") || !strings.Contains(event.Message, "hello from email") {
			t.Fatalf("unexpected event message: %q", event.Message)
		}
		if event.Meta["message_id"] != "<msg-101@example.com>" {
			t.Fatalf("unexpected event meta: %#v", event.Meta)
		}
		autoReply, ok := event.Meta["auto_reply_enabled"].(bool)
		if !ok || autoReply {
			t.Fatalf("expected auto_reply_enabled=false in event meta, got %#v", event.Meta)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for inbound email event")
	}
}

func TestChannel_StartSkipsValidationWhenConsentDisabled(t *testing.T) {
	config := baseConfig()
	config.ConsentGranted = false
	config.IMAPHost = ""
	config.SMTPHost = ""
	channel := &Channel{Config: config}

	if err := channel.Start(context.Background(), nil); err != nil {
		t.Fatalf("expected consent-disabled start to no-op, got %v", err)
	}
}

func TestChannel_DeduplicatesInboundMessages(t *testing.T) {
	config := baseConfig()
	fetches := 0
	channel := &Channel{
		Config: config,
		FetchMessages: func(ctx context.Context) ([]InboundMessage, error) {
			fetches++
			return []InboundMessage{{UID: "same-uid", From: "alice@example.com", Subject: "Hello", MessageID: "<same@example.com>", Body: "once"}}, nil
		},
	}
	eventBus := bus.New(2)
	channel.pollOnce(context.Background(), eventBus)
	channel.pollOnce(context.Background(), eventBus)
	if fetches != 2 {
		t.Fatalf("expected two fetches, got %d", fetches)
	}
	select {
	case <-eventBus.Channel():
	default:
		t.Fatal("expected first event")
	}
	select {
	case event := <-eventBus.Channel():
		t.Fatalf("expected dedupe to suppress second event, got %#v", event)
	default:
	}
}

func TestChannel_DeduplicatesInboundMessagesPersistedInDB(t *testing.T) {
	database := openEmailTestDB(t)
	payload, _ := json.Marshal(map[string]any{
		"channel": "email",
		"from":    "alice@example.com",
		"meta": map[string]any{
			"sender_email": "alice@example.com",
			"uid":          "same-uid",
			"message_id":   "<same@example.com>",
		},
	})
	if _, err := database.AppendMessage(context.Background(), "email:alice@example.com", "user", "hello", json.RawMessage(payload)); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	channel := &Channel{
		Config: baseConfig(),
		DB:     database,
		FetchMessages: func(ctx context.Context) ([]InboundMessage, error) {
			return []InboundMessage{{UID: "same-uid", From: "alice@example.com", Subject: "Hello", MessageID: "<same@example.com>", Body: "once"}}, nil
		},
	}
	eventBus := bus.New(1)
	channel.pollOnce(context.Background(), eventBus)
	select {
	case event := <-eventBus.Channel():
		t.Fatalf("expected persisted dedupe to suppress event, got %#v", event)
	default:
	}
}

func TestChannel_DeliverUsesThreadMetadataAndExplicitOverride(t *testing.T) {
	config := baseConfig()
	config.AutoReplyEnabled = true
	channel := &Channel{Config: config}
	channel.rememberThread("alice@example.com", "Project update", "<msg-101@example.com>")

	var outbound OutboundMessage
	channel.SendMail = func(ctx context.Context, message OutboundMessage) error {
		outbound = message
		return nil
	}

	if err := channel.Deliver(context.Background(), "alice@example.com", "reply text", nil); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if outbound.Subject != "Re: Project update" || outbound.InReplyTo != "<msg-101@example.com>" {
		t.Fatalf("unexpected threaded outbound message: %+v", outbound)
	}

	if err := channel.Deliver(context.Background(), "bob@example.com", "proactive", map[string]any{"subject": "Custom subject", "explicit_to": true}); err != nil {
		t.Fatalf("Deliver proactive: %v", err)
	}
	if outbound.To != "bob@example.com" || outbound.Subject != "Custom subject" {
		t.Fatalf("unexpected proactive outbound message: %+v", outbound)
	}
}

func TestChannel_DeliverDoesNotSuppressWhenAutoReplyDisabled(t *testing.T) {
	config := baseConfig()
	config.AutoReplyEnabled = false
	channel := &Channel{Config: config}
	channel.rememberThread("alice@example.com", "Subject", "<msg-101@example.com>")
	called := false
	channel.SendMail = func(ctx context.Context, message OutboundMessage) error {
		called = true
		return nil
	}

	if err := channel.Deliver(context.Background(), "alice@example.com", "reply text", nil); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if !called {
		t.Fatal("expected delivery to leave auto-reply policy to runtime")
	}
}

func TestChannel_RestoresThreadMetadataFromDB(t *testing.T) {
	database := openEmailTestDB(t)
	payload, _ := json.Marshal(map[string]any{
		"channel": "email",
		"from":    "alice@example.com",
		"meta": map[string]any{
			"sender_email": "alice@example.com",
			"subject":      "Stored subject",
			"message_id":   "<stored@example.com>",
		},
	})
	if _, err := database.AppendMessage(context.Background(), "email:alice@example.com", "user", "hello", json.RawMessage(payload)); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	config := baseConfig()
	config.AutoReplyEnabled = true
	channel := &Channel{Config: config, DB: database}
	var outbound OutboundMessage
	channel.SendMail = func(ctx context.Context, message OutboundMessage) error {
		outbound = message
		return nil
	}

	if err := channel.Deliver(context.Background(), "alice@example.com", "reply text", nil); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if outbound.Subject != "Re: Stored subject" || outbound.InReplyTo != "<stored@example.com>" {
		t.Fatalf("expected thread restoration from DB, got %+v", outbound)
	}
}

func TestChannel_SendViaSMTPRejectsPlaintextAuth(t *testing.T) {
	config := baseConfig()
	config.SMTPUseSSL = false
	config.SMTPUseTLS = false
	channel := &Channel{Config: config}
	err := channel.sendViaSMTP(context.Background(), OutboundMessage{
		To:      "alice@example.com",
		From:    "bot@example.com",
		Subject: "Subject",
		Text:    "Body",
	})
	if err == nil || !strings.Contains(err.Error(), "smtp auth requires TLS or SSL") {
		t.Fatalf("expected plaintext auth rejection, got %v", err)
	}
}

func TestChannel_FetchViaIMAPHonorsContextCancellation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer listener.Close()

	host, portValue, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("SplitHostPort: %v", err)
	}
	port, err := strconv.Atoi(portValue)
	if err != nil {
		t.Fatalf("Atoi: %v", err)
	}

	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			accepted <- conn
		}
	}()

	config := baseConfig()
	config.IMAPHost = host
	config.IMAPPort = port
	config.IMAPUseSSL = false
	channel := &Channel{Config: config}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := channel.fetchViaIMAP(ctx)
		done <- err
	}()

	var conn net.Conn
	select {
	case conn = <-accepted:
	case <-time.After(time.Second):
		t.Fatal("expected IMAP client to connect")
	}
	defer conn.Close()

	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected cancellation error")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected fetchViaIMAP to stop after context cancellation")
	}
}

func TestChannel_SendViaSMTPHonorsContextCancellationAfterConnect(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer listener.Close()

	host, portValue, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("SplitHostPort: %v", err)
	}
	port, err := strconv.Atoi(portValue)
	if err != nil {
		t.Fatalf("Atoi: %v", err)
	}

	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			accepted <- conn
		}
	}()

	config := baseConfig()
	config.SMTPHost = host
	config.SMTPPort = port
	config.SMTPUsername = ""
	config.SMTPPassword = ""
	config.SMTPUseSSL = false
	config.SMTPUseTLS = false
	channel := &Channel{Config: config}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- channel.sendViaSMTP(ctx, OutboundMessage{
			To:      "alice@example.com",
			From:    "bot@example.com",
			Subject: "Subject",
			Text:    "Body",
		})
	}()

	var conn net.Conn
	select {
	case conn = <-accepted:
	case <-time.After(time.Second):
		t.Fatal("expected SMTP client to connect")
	}
	defer conn.Close()

	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected cancellation error")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected sendViaSMTP to stop after context cancellation")
	}
}
