package mailer

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// SendTimeout — таймаут одной попытки отправки.
	SendTimeout = 10 * time.Second
	// RetryInterval — минимальный интервал между попытками.
	RetryInterval = time.Minute
	// scanInterval — период проверки аутбокса фоном.
	scanInterval = 15 * time.Second
	// cacheTTL — через сколько кэш настроек/ключа принудительно перечитать
	// (на случай ручной правки файлов). Актуализация — только при запросе.
	cacheTTL = 5 * time.Minute
)

// Event — событие почтовика для аудита.
type Event struct {
	Kind      string // enqueue | sent | failed
	Subject   string
	Recipient string
	Attempts  int
	Error     string
}

// Mailer — почтовик: настройки/секрет + аутбокс + фоновый отправитель.
type Mailer struct {
	cfgPath string
	keyPath string
	Outbox  *Outbox

	// OnEvent — необязательный колбэк для аудита.
	OnEvent func(Event)

	// Кэш (ключ, конфиг, расшифрованный секрет) под мутексом.
	mu        sync.Mutex
	key       []byte
	keyMod    time.Time
	keySize   int64
	keyAt     time.Time
	cfg       Config
	cfgMod    time.Time
	cfgSize   int64
	cfgAt     time.Time
	cfgOK     bool
	secret    string
	secretEnc string
	secretOK  bool

	stopOnce sync.Once
	stop     chan struct{}
}

func (m *Mailer) emit(e Event) {
	if m.OnEvent != nil {
		m.OnEvent(e)
	}
}

// New создаёт Mailer (пустые пути → дефолтные).
func New(cfgPath, keyPath, outboxDir string) *Mailer {
	if cfgPath == "" {
		cfgPath = DefaultConfigPath
	}
	if keyPath == "" {
		keyPath = DefaultKeyPath
	}
	if outboxDir == "" {
		outboxDir = DefaultOutboxDir
	}
	return &Mailer{
		cfgPath: cfgPath,
		keyPath: keyPath,
		Outbox:  &Outbox{Dir: outboxDir},
		stop:    make(chan struct{}),
	}
}

// LoadConfig — текущие настройки (из кэша, перечитывается при изменении файла).
func (m *Mailer) LoadConfig() (Config, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.currentConfigLocked()
}

func (m *Mailer) currentConfigLocked() (Config, error) {
	fi, err := os.Stat(m.cfgPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{Provider: "mailru", Security: string(SecSSL)}, nil
		}
		return Config{}, err
	}
	if m.cfgOK && time.Since(m.cfgAt) <= cacheTTL &&
		fi.ModTime().Equal(m.cfgMod) && fi.Size() == m.cfgSize {
		return m.cfg, nil
	}
	c, err := Load(m.cfgPath)
	if err != nil {
		return c, err
	}
	m.cfg, m.cfgMod, m.cfgSize, m.cfgAt, m.cfgOK = c, fi.ModTime(), fi.Size(), time.Now(), true
	return c, nil
}

// keyLocked — ключ из кэша; перечитывается при изменении/отсутствии файла. m.mu держится.
func (m *Mailer) keyLocked() ([]byte, error) {
	if m.key != nil && time.Since(m.keyAt) <= cacheTTL {
		if fi, err := os.Stat(m.keyPath); err == nil &&
			fi.ModTime().Equal(m.keyMod) && fi.Size() == m.keySize {
			return m.key, nil
		}
	}
	k, err := loadOrCreateKey(m.keyPath)
	if err != nil {
		return nil, err
	}
	m.key = k
	if fi, err := os.Stat(m.keyPath); err == nil {
		m.keyMod, m.keySize = fi.ModTime(), fi.Size()
	}
	m.keyAt = time.Now()
	m.secretOK = false // ключ мог смениться — секрет пересчитать
	return k, nil
}

// Invalidate — сбросить весь кэш (конфиг/ключ/секрет).
func (m *Mailer) Invalidate() {
	m.mu.Lock()
	m.key, m.cfg, m.secret = nil, Config{}, ""
	m.cfgOK, m.secretOK = false, false
	m.cfgMod, m.keyMod = time.Time{}, time.Time{}
	m.cfgAt, m.keyAt = time.Time{}, time.Time{}
	m.mu.Unlock()
}

// secretForLocked — расшифрованный секрет с кэшем по значению SecretEnc. m.mu держится.
func (m *Mailer) secretForLocked(c Config) (string, error) {
	if m.secretOK && m.secretEnc == c.SecretEnc {
		return m.secret, nil
	}
	key, err := m.keyLocked()
	if err != nil {
		return "", err
	}
	s, err := decryptWithKey(key, c.SecretEnc)
	if err != nil {
		return "", err
	}
	s = normalizeSecret(s)
	m.secret, m.secretEnc, m.secretOK = s, c.SecretEnc, true
	return s, nil
}

func (m *Mailer) secretFor(c Config) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.secretForLocked(c)
}

// SaveConfig сохраняет настройки; newSecret != "" — обновить секрет (кэш обновляется).
func (m *Mailer) SaveConfig(c Config, newSecret string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	ns := normalizeSecret(newSecret)
	if ns != "" {
		key, err := m.keyLocked()
		if err != nil {
			return err
		}
		enc, err := encryptWithKey(key, ns)
		if err != nil {
			return err
		}
		c.SecretEnc = enc
	}
	if err := Save(m.cfgPath, c); err != nil {
		return err
	}
	m.cfg, m.cfgOK = c, true
	if fi, err := os.Stat(m.cfgPath); err == nil {
		m.cfgMod, m.cfgSize = fi.ModTime(), fi.Size()
	}
	if ns != "" {
		m.secret, m.secretEnc, m.secretOK = ns, c.SecretEnc, true
	} else {
		m.secretOK = false
	}
	return nil
}

// SecretConfigured — задан ли секрет.
func (m *Mailer) SecretConfigured() bool {
	c, err := m.LoadConfig()
	return err == nil && c.SecretEnc != ""
}

// SendText — «простой текст» (в аутбокс). Возвращает id.
func (m *Mailer) SendText(subject, body string) (string, error) {
	return m.enqueue(subject, body, false)
}

// SendHTML — «HTML с параметрами» (в аутбокс). Возвращает id.
func (m *Mailer) SendHTML(subject, tmpl string, vars map[string]any) (string, error) {
	return m.enqueue(subject, renderTemplate(tmpl, vars), true)
}

func (m *Mailer) enqueue(subject, body string, html bool) (string, error) {
	c, err := m.LoadConfig()
	if err != nil {
		return "", err
	}
	if err := Validate(c); err != nil {
		return "", err
	}
	msg := Message{Subject: subject, Body: body, HTML: html, Recipient: c.Recipient}
	if err := m.Outbox.Add(msg); err != nil {
		return "", err
	}
	m.emit(Event{Kind: "enqueue", Subject: subject, Recipient: c.Recipient})
	return msg.ID, nil
}

// SendNow — синхронная отправка (тест/критичное), без аутбокса.
func (m *Mailer) SendNow(subject, body string, html bool) error {
	c, err := m.LoadConfig()
	if err != nil {
		return err
	}
	if err := Validate(c); err != nil {
		return err
	}
	secret, err := m.secretFor(c)
	if err != nil {
		return err
	}
	if err := sendSMTP(c, secret, Message{Subject: subject, Body: body, HTML: html, Recipient: c.Recipient}); err != nil {
		m.emit(Event{Kind: "failed", Subject: subject, Recipient: c.Recipient, Attempts: 1, Error: err.Error()})
		return err
	}
	m.emit(Event{Kind: "sent", Subject: subject, Recipient: c.Recipient, Attempts: 1})
	return nil
}

// Start — запустить фоновый отправитель.
func (m *Mailer) Start() { go m.loop() }

// Stop — остановить фоновый отправитель.
func (m *Mailer) Stop() { m.stopOnce.Do(func() { close(m.stop) }) }

func (m *Mailer) loop() {
	t := time.NewTicker(scanInterval)
	defer t.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-t.C:
			m.processDue()
		}
	}
}

func (m *Mailer) processDue() {
	list, err := m.Outbox.List()
	if err != nil {
		return
	}
	now := time.Now()
	for _, msg := range list {
		if msg.NextAttempt.After(now) {
			continue
		}
		m.attempt(msg)
	}
}

func (m *Mailer) attempt(msg Message) {
	c, err := m.LoadConfig()
	if err != nil {
		m.markFail(msg, err)
		return
	}
	if err := Validate(c); err != nil {
		m.markFail(msg, err)
		return
	}
	secret, err := m.secretFor(c)
	if err != nil {
		m.markFail(msg, err)
		return
	}
	if err := sendSMTP(c, secret, msg); err != nil {
		m.markFail(msg, err)
		return
	}
	_ = m.Outbox.Delete(msg.ID)
	m.emit(Event{Kind: "sent", Subject: msg.Subject, Recipient: msg.Recipient, Attempts: msg.Attempts + 1})
}

func (m *Mailer) markFail(msg Message, err error) {
	msg.Attempts++
	msg.LastError = err.Error()
	msg.NextAttempt = time.Now().Add(RetryInterval)
	_ = m.Outbox.write(msg)
	m.emit(Event{Kind: "failed", Subject: msg.Subject, Recipient: msg.Recipient, Attempts: msg.Attempts, Error: err.Error()})
}

// connectAny перебирает все адреса хоста с коротким таймаутом на каждый
// (часть SMTP-IP может «чернодырить» по TCP и/или TLS), IPv4 — первыми.
// Для SSL рукопожатие делается внутри попытки, чтобы пропускать «мёртвые» IP.
func connectAny(host string, port int, sec Security) (net.Conn, error) {
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, err
	}
	// IPv4 первыми; IPv6 — только если IPv4 нет вовсе (у устройства обычно нет v6).
	var v4, v6 []net.IP
	for _, ip := range ips {
		if ip.To4() != nil {
			v4 = append(v4, ip)
		} else {
			v6 = append(v6, ip)
		}
	}
	if len(v4) > 0 {
		ips = v4
	} else {
		ips = v6
	}
	p := strconv.Itoa(port)
	deadline := time.Now().Add(SendTimeout)
	var lastErr error
	for _, ip := range ips {
		remain := time.Until(deadline)
		if remain <= 0 {
			break
		}
		per := 4 * time.Second
		if per > remain {
			per = remain
		}
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(ip.String(), p), per)
		if err != nil {
			lastErr = err
			continue
		}
		if sec == SecSSL {
			tc := tls.Client(conn, &tls.Config{ServerName: host})
			_ = tc.SetDeadline(time.Now().Add(per))
			if err := tc.Handshake(); err != nil {
				_ = conn.Close()
				lastErr = err
				continue
			}
			_ = tc.SetDeadline(time.Time{})
			return tc, nil
		}
		return conn, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("нет доступных адресов")
	}
	return nil, lastErr
}

func sendSMTP(c Config, secret string, msg Message) error {
	host, port, sec := c.Effective()
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := connectAny(host, port, sec)
	if err != nil {
		return fmt.Errorf("connect %s: %w", addr, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(SendTimeout))

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("smtp hello: %w", err)
	}
	defer client.Close()

	if sec == SecSTARTTLS {
		if err := client.StartTLS(&tls.Config{ServerName: host}); err != nil {
			return fmt.Errorf("starttls: %w", err)
		}
	}
	if secret != "" {
		if err := client.Auth(smtp.PlainAuth("", c.Username, secret, host)); err != nil {
			return fmt.Errorf("auth: %w", err)
		}
	}
	if err := client.Mail(c.From); err != nil {
		return fmt.Errorf("mail from: %w", err)
	}
	if err := client.Rcpt(msg.Recipient); err != nil {
		return fmt.Errorf("rcpt to: %w", err)
	}
	wc, err := client.Data()
	if err != nil {
		return fmt.Errorf("data: %w", err)
	}
	if _, err := wc.Write(buildMessage(c, msg)); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("quit: %w", err)
	}
	return nil
}

func buildMessage(c Config, msg Message) []byte {
	var b bytes.Buffer
	b.WriteString("From: " + c.From + "\r\n")
	b.WriteString("To: " + msg.Recipient + "\r\n")
	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", msg.Subject) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	if msg.HTML {
		b.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	} else {
		b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	}
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
	body := strings.ReplaceAll(msg.Body, "\n", "\r\n")
	if !strings.HasSuffix(body, "\r\n") {
		body += "\r\n"
	}
	b.WriteString(body)
	return b.Bytes()
}

func renderTemplate(tmpl string, vars map[string]any) string {
	out := tmpl
	for k, v := range vars {
		out = strings.ReplaceAll(out, "{{"+k+"}}", fmt.Sprintf("%v", v))
	}
	return out
}
