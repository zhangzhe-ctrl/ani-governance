// Package mailer 提供基于 net/smtp 的邮件发送能力。
// 支持 STARTTLS（587/25）与隐式 SSL/TLS（465）两种加密方式，
// 认证使用 SMTP PLAIN 机制（覆盖常见邮箱服务商的授权码模式）。
package mailer

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"strings"
)

// SmtpConfig SMTP 连接配置
type SmtpConfig struct {
	Host     string // 服务器地址
	Port     uint32 // 端口
	Username string // 用户名
	Password string // 密码/授权码
	From     string // 发件人地址
	TlsMode  string // NONE / START_TLS / SSL
}

// SendMail 通过 SMTP 发送一封纯文本邮件。
// to 可为多个收件人。
func SendMail(cfg SmtpConfig, to []string, subject, body string) error {
	if cfg.Host == "" || cfg.Port == 0 {
		return fmt.Errorf("smtp host/port is not configured")
	}
	if len(to) == 0 {
		return fmt.Errorf("recipient is empty")
	}
	if cfg.From == "" {
		cfg.From = cfg.Username
	}

	addr := cfg.Host + ":" + strconv.Itoa(int(cfg.Port))
	from := strings.TrimSpace(cfg.From)

	msg := buildMessage(from, to, subject, body)

	var client *smtp.Client
	var err error

	switch strings.ToUpper(cfg.TlsMode) {
	case "SSL":
		client, err = dialSSL(addr, cfg.Host)
	case "NONE", "START_TLS", "":
		// NONE：明文连接（内网调试 SMTP 常见）；服务器支持 STARTTLS 时自动升级
		client, err = dialStartTLS(addr, cfg.Host)
	default:
		return fmt.Errorf("unsupported tls mode: %s", cfg.TlsMode)
	}
	if err != nil {
		return fmt.Errorf("connect smtp server failed: %w", err)
	}
	defer client.Close()

	if cfg.Username != "" {
		auth := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
		if err = client.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth failed: %w", err)
		}
	}

	if err = client.Mail(from); err != nil {
		return fmt.Errorf("smtp MAIL failed: %w", err)
	}
	for _, rcpt := range to {
		if err = client.Rcpt(rcpt); err != nil {
			return fmt.Errorf("smtp RCPT failed for %s: %w", rcpt, err)
		}
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp DATA failed: %w", err)
	}
	if _, err = w.Write(msg); err != nil {
		return fmt.Errorf("write message failed: %w", err)
	}
	if err = w.Close(); err != nil {
		return fmt.Errorf("close message failed: %w", err)
	}

	return client.Quit()
}

func dialSSL(addr, host string) (*smtp.Client, error) {
	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host})
	if err != nil {
		return nil, err
	}
	return smtp.NewClient(conn, host)
}

func dialStartTLS(addr, host string) (*smtp.Client, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if ok, _ := client.Extension("STARTTLS"); ok {
		if err = client.StartTLS(&tls.Config{ServerName: host}); err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("STARTTLS failed: %w", err)
		}
	}
	// 服务器不支持 STARTTLS 时按明文继续（内网调试 SMTP 常见）
	return client, nil
}

func buildMessage(from string, to []string, subject, body string) []byte {
	header := make([]byte, 0, 512+len(subject)+len(body))
	header = append(header, "From: "...)
	header = append(header, from...)
	header = append(header, "\r\n"...)
	header = append(header, "To: "...)
	header = append(header, strings.Join(to, ", ")...)
	header = append(header, "\r\n"...)
	header = append(header, "Subject: "...)
	header = append(header, subject...)
	header = append(header, "\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n"...)
	header = append(header, body...)
	header = append(header, "\r\n"...)
	return header
}
