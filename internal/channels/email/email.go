// Package email implements the email channel adapter.
package email

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	xhtml "golang.org/x/net/html"

	"or3-intern/internal/bus"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
)

const (
	defaultPollInterval = 30 * time.Second
	defaultNetTimeout   = 30 * time.Second
	maxFetchBatch       = 20
	maxProcessedKeys    = 4096
	dedupeMessageLimit  = 200
	lookupMessageLimit  = 50
)

// InboundMessage is a normalized email fetched from IMAP.
type InboundMessage struct {
	UID       string
	From      string
	Subject   string
	MessageID string
	Date      time.Time
	Body      string
}

// OutboundMessage is an email prepared for SMTP delivery.
type OutboundMessage struct {
	To        string
	From      string
	Subject   string
	Text      string
	InReplyTo string
}

type threadState struct {
	Subject   string
	MessageID string
}

// Channel polls inbound email and sends outbound replies.
type Channel struct {
	Config config.EmailChannelConfig
	DB     *db.DB

	FetchMessages func(ctx context.Context) ([]InboundMessage, error)
	SendMail      func(ctx context.Context, outbound OutboundMessage) error

	mu             sync.Mutex
	running        bool
	cancel         context.CancelFunc
	threadBySender map[string]threadState
	processedKeys  map[string]struct{}
	processedOrder []string
}

// Name returns the registered channel name.
func (c *Channel) Name() string { return "email" }

// Start validates configuration and begins polling for inbound mail.
func (c *Channel) Start(ctx context.Context, eventBus *bus.Bus) error {
	if err := c.validate(); err != nil {
		return err
	}
	if eventBus == nil {
		return fmt.Errorf("event bus not configured")
	}
	if !c.Config.ConsentGranted {
		log.Printf("email channel disabled: consentGranted is false")
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running {
		return nil
	}
	if c.threadBySender == nil {
		c.threadBySender = map[string]threadState{}
	}
	if c.processedKeys == nil {
		c.processedKeys = map[string]struct{}{}
	}
	childCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.running = true
	go c.pollLoop(childCtx, eventBus)
	return nil
}

// Stop cancels polling and leaves any current request to drain.
func (c *Channel) Stop(ctx context.Context) error {
	_ = ctx
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
	c.cancel = nil
	c.running = false
	return nil
}

// Deliver sends a reply or new outbound email.
func (c *Channel) Deliver(ctx context.Context, to, text string, meta map[string]any) error {
	recipient := normalizeAddress(to)
	if recipient == "" {
		recipient = normalizeAddress(c.Config.DefaultTo)
	}
	if recipient == "" {
		return fmt.Errorf("email recipient address required")
	}

	thread, _, err := c.lookupThread(ctx, recipient)
	if err != nil {
		return err
	}

	subject := c.subjectForDelivery(meta, thread.Subject)
	message := OutboundMessage{
		To:        recipient,
		From:      c.fromAddress(),
		Subject:   subject,
		Text:      text,
		InReplyTo: thread.MessageID,
	}
	return c.sendMail(ctx, message)
}

func (c *Channel) pollLoop(ctx context.Context, eventBus *bus.Bus) {
	interval := time.Duration(c.Config.PollIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = defaultPollInterval
	}
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}

	c.pollOnce(ctx, eventBus)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.pollOnce(ctx, eventBus)
		}
	}
}

func (c *Channel) pollOnce(ctx context.Context, eventBus *bus.Bus) {
	messages, err := c.fetchMessages(ctx)
	if err != nil {
		log.Printf("email polling error: %v", err)
		return
	}
	for _, inbound := range messages {
		sender := normalizeAddress(inbound.From)
		if sender == "" {
			continue
		}
		if c.alreadyProcessed(inbound.UID, inbound.MessageID) {
			continue
		}
		if !c.allowedSender(sender) {
			c.rememberProcessed(inbound.UID, inbound.MessageID)
			log.Printf("email sender ignored: %s", sender)
			continue
		}
		persisted, err := c.alreadyProcessedPersisted(ctx, sender, inbound.UID, inbound.MessageID)
		if err != nil {
			log.Printf("email dedupe lookup failed for %s: %v", sender, err)
		} else if persisted {
			c.rememberProcessed(inbound.UID, inbound.MessageID)
			continue
		}
		c.rememberProcessed(inbound.UID, inbound.MessageID)
		c.rememberThread(sender, inbound.Subject, inbound.MessageID)
		meta := map[string]any{
			"sender_email":       sender,
			"subject":            strings.TrimSpace(inbound.Subject),
			"message_id":         strings.TrimSpace(inbound.MessageID),
			"uid":                strings.TrimSpace(inbound.UID),
			"auto_reply_enabled": c.Config.AutoReplyEnabled,
		}
		if !inbound.Date.IsZero() {
			meta["date"] = inbound.Date.Format(time.RFC3339)
		}
		ok := eventBus.Publish(bus.Event{
			Type:       bus.EventUserMessage,
			SessionKey: "email:" + sender,
			Channel:    "email",
			From:       sender,
			Message:    formatInboundMessage(sender, inbound.Subject, inbound.Date, inbound.Body),
			Meta:       meta,
		})
		if !ok {
			log.Printf("email event dropped: queue full for %s", sender)
		}
	}
}

func (c *Channel) validate() error {
	if !c.Config.Enabled {
		return nil
	}
	if !c.Config.OpenAccess && !hasNonEmpty(c.Config.AllowedSenders) {
		return fmt.Errorf("email enabled: set allowedSenders or openAccess=true")
	}
	if strings.TrimSpace(c.Config.IMAPHost) == "" || strings.TrimSpace(c.Config.IMAPUsername) == "" || strings.TrimSpace(c.Config.IMAPPassword) == "" {
		return fmt.Errorf("email requires IMAP host, username, and password")
	}
	if strings.TrimSpace(c.Config.SMTPHost) == "" || strings.TrimSpace(c.Config.SMTPUsername) == "" || strings.TrimSpace(c.Config.SMTPPassword) == "" {
		return fmt.Errorf("email requires SMTP host, username, and password")
	}
	return nil
}

func (c *Channel) fetchMessages(ctx context.Context) ([]InboundMessage, error) {
	if c.FetchMessages != nil {
		return c.FetchMessages(ctx)
	}
	return c.fetchViaIMAP(ctx)
}

func (c *Channel) sendMail(ctx context.Context, outbound OutboundMessage) error {
	if c.SendMail != nil {
		return c.SendMail(ctx, outbound)
	}
	return c.sendViaSMTP(ctx, outbound)
}

func (c *Channel) fetchViaIMAP(ctx context.Context) ([]InboundMessage, error) {
	client, stopWatch, err := c.dialIMAP(ctx)
	if err != nil {
		return nil, err
	}
	defer stopWatch()
	defer client.Close()
	if err := client.Login(c.Config.IMAPUsername, c.Config.IMAPPassword).Wait(); err != nil {
		return nil, fmt.Errorf("imap login: %w", err)
	}
	defer func() {
		if err := client.Logout().Wait(); err != nil {
			log.Printf("email logout error: %v", err)
		}
	}()

	mailbox := strings.TrimSpace(c.Config.IMAPMailbox)
	if mailbox == "" {
		mailbox = "INBOX"
	}
	if _, err := client.Select(mailbox, nil).Wait(); err != nil {
		return nil, fmt.Errorf("imap select %s: %w", mailbox, err)
	}

	criteria := &imap.SearchCriteria{NotFlag: []imap.Flag{imap.FlagSeen}}
	searchData, err := client.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return nil, fmt.Errorf("imap search: %w", err)
	}
	uids := searchData.AllUIDs()
	if len(uids) == 0 {
		return nil, nil
	}
	sort.Slice(uids, func(i, j int) bool { return uids[i] < uids[j] })
	if len(uids) > maxFetchBatch {
		uids = uids[len(uids)-maxFetchBatch:]
	}

	var uidSet imap.UIDSet
	uidSet.AddNum(uids...)
	bodySection := &imap.FetchItemBodySection{Peek: true}
	messages, err := client.Fetch(uidSet, &imap.FetchOptions{
		UID:         true,
		Envelope:    true,
		BodySection: []*imap.FetchItemBodySection{bodySection},
	}).Collect()
	if err != nil {
		return nil, fmt.Errorf("imap fetch: %w", err)
	}
	sort.Slice(messages, func(i, j int) bool { return messages[i].UID < messages[j].UID })

	out := make([]InboundMessage, 0, len(messages))
	markedUIDs := make([]imap.UID, 0, len(messages))
	for _, message := range messages {
		raw := message.FindBodySection(bodySection)
		if len(raw) == 0 {
			continue
		}
		parsed, err := parseRawEmail(raw, c.Config.MaxBodyChars)
		if err != nil {
			log.Printf("email parse error for uid=%d: %v", message.UID, err)
			continue
		}
		parsed.UID = fmt.Sprintf("%d", message.UID)
		out = append(out, parsed)
		if c.Config.MarkSeen {
			markedUIDs = append(markedUIDs, message.UID)
		}
	}
	if c.Config.MarkSeen && len(markedUIDs) > 0 {
		var seenSet imap.UIDSet
		seenSet.AddNum(markedUIDs...)
		storeFlags := &imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagSeen}}
		if err := client.Store(seenSet, storeFlags, nil).Close(); err != nil {
			log.Printf("email mark seen error: %v", err)
		}
	}
	return out, nil
}

func (c *Channel) dialIMAP(ctx context.Context) (*imapclient.Client, func(), error) {
	address := net.JoinHostPort(strings.TrimSpace(c.Config.IMAPHost), fmt.Sprintf("%d", c.Config.IMAPPort))
	dialer := &net.Dialer{Timeout: defaultNetTimeout}
	var conn net.Conn
	var err error
	if c.Config.IMAPUseSSL {
		baseConn, dialErr := dialer.DialContext(ctx, "tcp", address)
		if dialErr != nil {
			return nil, func() {}, dialErr
		}
		tlsConn := tls.Client(baseConn, &tls.Config{ServerName: strings.TrimSpace(c.Config.IMAPHost)})
		if deadline, ok := connectionDeadline(ctx); ok {
			_ = tlsConn.SetDeadline(deadline)
		}
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = baseConn.Close()
			return nil, func() {}, fmt.Errorf("imap tls handshake: %w", err)
		}
		conn = tlsConn
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", address)
		if err != nil {
			return nil, func() {}, err
		}
	}
	if deadline, ok := connectionDeadline(ctx); ok {
		_ = conn.SetDeadline(deadline)
	}
	stopWatch := watchConnContext(ctx, conn)
	client := imapclient.New(conn, nil)
	if err := client.WaitGreeting(); err != nil {
		stopWatch()
		_ = client.Close()
		return nil, func() {}, fmt.Errorf("imap greeting: %w", err)
	}
	return client, stopWatch, nil
}

func (c *Channel) sendViaSMTP(ctx context.Context, outbound OutboundMessage) error {
	if err := c.validateSMTPAuthTransport(); err != nil {
		return err
	}
	messageBytes, err := buildOutboundMessage(outbound)
	if err != nil {
		return err
	}
	address := net.JoinHostPort(strings.TrimSpace(c.Config.SMTPHost), fmt.Sprintf("%d", c.Config.SMTPPort))
	dialer := &net.Dialer{Timeout: 30 * time.Second}

	var conn net.Conn
	if c.Config.SMTPUseSSL {
		conn, err = tls.DialWithDialer(dialer, "tcp", address, &tls.Config{ServerName: strings.TrimSpace(c.Config.SMTPHost)})
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return fmt.Errorf("smtp dial: %w", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, strings.TrimSpace(c.Config.SMTPHost))
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer client.Close()

	if c.Config.SMTPUseTLS && !c.Config.SMTPUseSSL {
		if err := client.StartTLS(&tls.Config{ServerName: strings.TrimSpace(c.Config.SMTPHost)}); err != nil {
			return fmt.Errorf("smtp starttls: %w", err)
		}
	}
	if strings.TrimSpace(c.Config.SMTPUsername) != "" {
		auth := smtp.PlainAuth("", c.Config.SMTPUsername, c.Config.SMTPPassword, strings.TrimSpace(c.Config.SMTPHost))
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := client.Mail(outbound.From); err != nil {
		return fmt.Errorf("smtp mail from: %w", err)
	}
	if err := client.Rcpt(outbound.To); err != nil {
		return fmt.Errorf("smtp rcpt to: %w", err)
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := writer.Write(messageBytes); err != nil {
		_ = writer.Close()
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("smtp finalize: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("smtp quit: %w", err)
	}
	return nil
}

func buildOutboundMessage(outbound OutboundMessage) ([]byte, error) {
	from := strings.TrimSpace(outbound.From)
	to := normalizeAddress(outbound.To)
	if from == "" || to == "" {
		return nil, fmt.Errorf("email from and to addresses are required")
	}
	subject := strings.TrimSpace(outbound.Subject)
	if subject == "" {
		subject = "or3-intern reply"
	}
	body := strings.ReplaceAll(outbound.Text, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\r", "\n")

	var buf bytes.Buffer
	headers := []string{
		"From: " + from,
		"To: " + to,
		"Subject: " + mime.QEncoding.Encode("utf-8", subject),
		"Date: " + time.Now().Format(time.RFC1123Z),
		fmt.Sprintf("Message-ID: <%d.%s>", time.Now().UnixNano(), strings.ReplaceAll(strings.SplitN(from, "@", 2)[0], " ", "-")),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: quoted-printable",
	}
	if strings.TrimSpace(outbound.InReplyTo) != "" {
		headers = append(headers, "In-Reply-To: "+strings.TrimSpace(outbound.InReplyTo))
		headers = append(headers, "References: "+strings.TrimSpace(outbound.InReplyTo))
	}
	for _, headerLine := range headers {
		buf.WriteString(headerLine)
		buf.WriteString("\r\n")
	}
	buf.WriteString("\r\n")
	quoted := quotedprintable.NewWriter(&buf)
	if _, err := quoted.Write([]byte(body)); err != nil {
		return nil, err
	}
	if err := quoted.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func parseRawEmail(raw []byte, maxBodyChars int) (InboundMessage, error) {
	message, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return InboundMessage{}, err
	}
	parsed := InboundMessage{
		From:      normalizeAddress(message.Header.Get("From")),
		Subject:   decodeHeaderValue(message.Header.Get("Subject")),
		MessageID: strings.TrimSpace(message.Header.Get("Message-ID")),
	}
	if dateValue := strings.TrimSpace(message.Header.Get("Date")); dateValue != "" {
		if parsedDate, err := mail.ParseDate(dateValue); err == nil {
			parsed.Date = parsedDate
		}
	}
	parsed.Body = extractBodyText(textproto.MIMEHeader(message.Header), message.Body, maxBodyChars)
	return parsed, nil
}

func extractBodyText(header textproto.MIMEHeader, body io.Reader, maxBodyChars int) string {
	plain, htmlBodies := extractEntityBodies(header, body, maxBodyChars)
	if len(plain) > 0 {
		return truncateText(strings.Join(plain, "\n\n"), maxBodyChars)
	}
	if len(htmlBodies) > 0 {
		return truncateText(strings.Join(htmlBodies, "\n\n"), maxBodyChars)
	}
	return ""
}

func extractEntityBodies(header textproto.MIMEHeader, body io.Reader, maxBodyChars int) ([]string, []string) {
	mediaType, params, err := mime.ParseMediaType(header.Get("Content-Type"))
	if err != nil || mediaType == "" {
		mediaType = "text/plain"
	}
	disposition, _, _ := mime.ParseMediaType(header.Get("Content-Disposition"))
	if strings.EqualFold(disposition, "attachment") {
		return nil, nil
	}

	if strings.HasPrefix(strings.ToLower(mediaType), "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return nil, nil
		}
		reader := multipart.NewReader(decodeTransferEncoding(body, header.Get("Content-Transfer-Encoding")), boundary)
		plainParts := []string{}
		htmlParts := []string{}
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				break
			}
			childPlain, childHTML := extractEntityBodies(part.Header, part, maxBodyChars)
			plainParts = append(plainParts, childPlain...)
			htmlParts = append(htmlParts, childHTML...)
		}
		return plainParts, htmlParts
	}

	decodedBody, err := io.ReadAll(io.LimitReader(decodeTransferEncoding(body, header.Get("Content-Transfer-Encoding")), int64(maxReadBytes(maxBodyChars))))
	if err != nil {
		return nil, nil
	}
	text := strings.TrimSpace(string(decodedBody))
	if text == "" {
		return nil, nil
	}
	switch strings.ToLower(mediaType) {
	case "text/plain":
		return []string{normalizeText(text)}, nil
	case "text/html":
		return nil, []string{normalizeText(htmlToText(text))}
	default:
		return nil, nil
	}
}

func decodeTransferEncoding(body io.Reader, encoding string) io.Reader {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "base64":
		return base64.NewDecoder(base64.StdEncoding, body)
	case "quoted-printable":
		return quotedprintable.NewReader(body)
	default:
		return body
	}
}

func maxReadBytes(maxBodyChars int) int {
	if maxBodyChars <= 0 {
		return 8192
	}
	return maxBodyChars*4 + 1024
}

func htmlToText(input string) string {
	tokenizer := xhtml.NewTokenizer(strings.NewReader(input))
	segments := make([]string, 0, 16)
	appendText := func(text string) {
		text = strings.Join(strings.Fields(html.UnescapeString(text)), " ")
		if text == "" {
			return
		}
		if len(segments) > 0 {
			last := segments[len(segments)-1]
			if !strings.HasSuffix(last, "\n") && last != " " {
				segments = append(segments, " ")
			}
		}
		segments = append(segments, text)
	}
	appendBreak := func(double bool) {
		if len(segments) == 0 {
			return
		}
		want := "\n"
		if double {
			want = "\n\n"
		}
		last := segments[len(segments)-1]
		if strings.HasSuffix(last, "\n\n") || (!double && strings.HasSuffix(last, "\n")) {
			return
		}
		segments = append(segments, want)
	}
	for {
		tokenType := tokenizer.Next()
		switch tokenType {
		case xhtml.ErrorToken:
			return html.UnescapeString(strings.Join(segments, ""))
		case xhtml.TextToken:
			appendText(string(tokenizer.Text()))
		case xhtml.StartTagToken, xhtml.EndTagToken, xhtml.SelfClosingTagToken:
			name, _ := tokenizer.TagName()
			switch strings.ToLower(string(name)) {
			case "br":
				appendBreak(false)
			case "p", "div", "section", "article", "header", "footer", "aside", "li", "tr",
				"h1", "h2", "h3", "h4", "h5", "h6":
				appendBreak(true)
			}
		}
	}
}

func normalizeText(input string) string {
	text := strings.ReplaceAll(input, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	cleaned := make([]string, 0, len(lines))
	blankCount := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			blankCount++
			if blankCount > 1 {
				continue
			}
			cleaned = append(cleaned, "")
			continue
		}
		blankCount = 0
		cleaned = append(cleaned, trimmed)
	}
	return strings.TrimSpace(strings.Join(cleaned, "\n"))
}

func truncateText(text string, maxBodyChars int) string {
	text = strings.TrimSpace(text)
	if maxBodyChars > 0 && len(text) > maxBodyChars {
		return strings.TrimSpace(text[:maxBodyChars]) + "…"
	}
	return text
}

func formatInboundMessage(sender, subject string, sentAt time.Time, body string) string {
	lines := []string{"From: " + sender}
	if strings.TrimSpace(subject) != "" {
		lines = append(lines, "Subject: "+strings.TrimSpace(subject))
	}
	if !sentAt.IsZero() {
		lines = append(lines, "Date: "+sentAt.Format(time.RFC1123Z))
	}
	if strings.TrimSpace(body) != "" {
		lines = append(lines, "", strings.TrimSpace(body))
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func (c *Channel) allowedSender(sender string) bool {
	if c.Config.OpenAccess {
		return true
	}
	for _, allowed := range c.Config.AllowedSenders {
		if normalizeAddress(allowed) == sender {
			return true
		}
	}
	return false
}

func normalizeAddress(value string) string {
	parsed, err := mail.ParseAddress(strings.TrimSpace(value))
	if err == nil {
		return strings.ToLower(strings.TrimSpace(parsed.Address))
	}
	if strings.Contains(value, "<") && strings.Contains(value, ">") {
		start := strings.LastIndex(value, "<")
		end := strings.LastIndex(value, ">")
		if start >= 0 && end > start {
			value = value[start+1 : end]
		}
	}
	return strings.ToLower(strings.TrimSpace(value))
}

func decodeHeaderValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	decoder := new(mime.WordDecoder)
	decoded, err := decoder.DecodeHeader(value)
	if err != nil {
		return value
	}
	return strings.TrimSpace(decoded)
}

func (c *Channel) rememberProcessed(uid, messageID string) {
	keys := []string{}
	if strings.TrimSpace(uid) != "" {
		keys = append(keys, "uid:"+strings.TrimSpace(uid))
	}
	if strings.TrimSpace(messageID) != "" {
		keys = append(keys, "msgid:"+strings.TrimSpace(messageID))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.processedKeys == nil {
		c.processedKeys = map[string]struct{}{}
	}
	for _, key := range keys {
		if _, exists := c.processedKeys[key]; exists {
			continue
		}
		c.processedKeys[key] = struct{}{}
		c.processedOrder = append(c.processedOrder, key)
	}
	for len(c.processedOrder) > maxProcessedKeys {
		oldest := c.processedOrder[0]
		c.processedOrder = c.processedOrder[1:]
		delete(c.processedKeys, oldest)
	}
}

func (c *Channel) alreadyProcessed(uid, messageID string) bool {
	keys := []string{}
	if strings.TrimSpace(uid) != "" {
		keys = append(keys, "uid:"+strings.TrimSpace(uid))
	}
	if strings.TrimSpace(messageID) != "" {
		keys = append(keys, "msgid:"+strings.TrimSpace(messageID))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, key := range keys {
		if _, exists := c.processedKeys[key]; exists {
			return true
		}
	}
	return false
}

func (c *Channel) alreadyProcessedPersisted(ctx context.Context, sender, uid, messageID string) (bool, error) {
	if c.DB == nil {
		return false, nil
	}
	uid = strings.TrimSpace(uid)
	messageID = strings.TrimSpace(messageID)
	if uid == "" && messageID == "" {
		return false, nil
	}
	messages, err := c.DB.GetLastMessages(ctx, "email:"+sender, dedupeMessageLimit)
	if err != nil {
		return false, err
	}
	for _, message := range messages {
		if message.Role != "user" || strings.TrimSpace(message.PayloadJSON) == "" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(message.PayloadJSON), &payload); err != nil {
			continue
		}
		meta, _ := payload["meta"].(map[string]any)
		if len(meta) == 0 {
			continue
		}
		storedUID := strings.TrimSpace(fmt.Sprint(meta["uid"]))
		storedMessageID := strings.TrimSpace(fmt.Sprint(meta["message_id"]))
		if uid != "" && storedUID == uid {
			return true, nil
		}
		if messageID != "" && storedMessageID == messageID {
			return true, nil
		}
	}
	return false, nil
}

func (c *Channel) rememberThread(sender, subject, messageID string) {
	sender = normalizeAddress(sender)
	if sender == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.threadBySender == nil {
		c.threadBySender = map[string]threadState{}
	}
	state := c.threadBySender[sender]
	if strings.TrimSpace(subject) != "" {
		state.Subject = strings.TrimSpace(subject)
	}
	if strings.TrimSpace(messageID) != "" {
		state.MessageID = strings.TrimSpace(messageID)
	}
	c.threadBySender[sender] = state
}

func (c *Channel) lookupThread(ctx context.Context, recipient string) (threadState, bool, error) {
	recipient = normalizeAddress(recipient)
	c.mu.Lock()
	state, ok := c.threadBySender[recipient]
	c.mu.Unlock()
	if ok && (state.Subject != "" || state.MessageID != "") {
		return state, true, nil
	}
	if c.DB == nil {
		return threadState{}, false, nil
	}
	messages, err := c.DB.GetLastMessages(ctx, "email:"+recipient, lookupMessageLimit)
	if err != nil {
		return threadState{}, false, err
	}
	for idx := len(messages) - 1; idx >= 0; idx-- {
		message := messages[idx]
		var payload map[string]any
		if err := json.Unmarshal([]byte(message.PayloadJSON), &payload); err != nil {
			continue
		}
		if strings.TrimSpace(fmt.Sprint(payload["channel"])) != "email" {
			continue
		}
		meta, _ := payload["meta"].(map[string]any)
		if len(meta) == 0 {
			continue
		}
		state = threadState{
			Subject:   strings.TrimSpace(fmt.Sprint(meta["subject"])),
			MessageID: strings.TrimSpace(fmt.Sprint(meta["message_id"])),
		}
		if state.Subject != "" || state.MessageID != "" {
			c.rememberThread(recipient, state.Subject, state.MessageID)
			return state, true, nil
		}
	}
	return threadState{}, false, nil
}

func (c *Channel) subjectForDelivery(meta map[string]any, base string) string {
	if override := strings.TrimSpace(fmt.Sprint(meta["subject"])); override != "" && override != "<nil>" {
		return override
	}
	base = strings.TrimSpace(base)
	if base == "" {
		return "or3-intern reply"
	}
	lower := strings.ToLower(base)
	if strings.HasPrefix(lower, "re:") {
		return base
	}
	prefix := strings.TrimSpace(c.Config.SubjectPrefix)
	if prefix == "" {
		prefix = "Re:"
	}
	if !strings.HasSuffix(prefix, ":") && !strings.HasSuffix(prefix, " ") {
		prefix += " "
	}
	if !strings.HasSuffix(prefix, " ") {
		prefix += " "
	}
	return prefix + base
}

func (c *Channel) fromAddress() string {
	if value := normalizeAddress(c.Config.FromAddress); value != "" {
		return value
	}
	if value := normalizeAddress(c.Config.SMTPUsername); value != "" {
		return value
	}
	return normalizeAddress(c.Config.IMAPUsername)
}

func hasNonEmpty(values []string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func (c *Channel) validateSMTPAuthTransport() error {
	if strings.TrimSpace(c.Config.SMTPUsername) == "" {
		return nil
	}
	if c.Config.SMTPUseSSL || c.Config.SMTPUseTLS {
		return nil
	}
	return fmt.Errorf("smtp auth requires TLS or SSL")
}

func connectionDeadline(ctx context.Context) (time.Time, bool) {
	if ctx == nil {
		return time.Now().Add(defaultNetTimeout), true
	}
	if deadline, ok := ctx.Deadline(); ok {
		return deadline, true
	}
	return time.Now().Add(defaultNetTimeout), true
}

func watchConnContext(ctx context.Context, conn net.Conn) func() {
	if ctx == nil || conn == nil {
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	return func() {
		close(done)
	}
}
