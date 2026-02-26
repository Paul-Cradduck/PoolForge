package safety

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/smtp"
	"strings"
	"time"
)

type AlertLevel string

const (
	AlertInfo     AlertLevel = "info"
	AlertWarning  AlertLevel = "warning"
	AlertCritical AlertLevel = "critical"
)

type Alert struct {
	Level     AlertLevel `json:"level"`
	Pool      string     `json:"pool"`
	Device    string     `json:"device,omitempty"`
	Message   string     `json:"message"`
	Timestamp time.Time  `json:"timestamp"`
}

type AlertConfig struct {
	WebhookURL string
	SMTPHost   string
	SMTPPort   int
	SMTPFrom   string
	SMTPTo     []string
	SMTPUser   string
	SMTPPass   string
}

type Alerter struct {
	config  AlertConfig
	history []Alert
}

func NewAlerter(cfg AlertConfig) *Alerter {
	return &Alerter{config: cfg}
}

func (a *Alerter) Send(alert Alert) error {
	alert.Timestamp = time.Now()
	a.history = append(a.history, alert)
	// Keep last 100
	if len(a.history) > 100 {
		a.history = a.history[len(a.history)-100:]
	}

	var errs []string
	if a.config.WebhookURL != "" {
		if err := a.sendWebhook(alert); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if a.config.SMTPHost != "" && len(a.config.SMTPTo) > 0 {
		if err := a.sendEmail(alert); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

func (a *Alerter) History() []Alert { return a.history }

func (a *Alerter) sendWebhook(alert Alert) error {
	data, _ := json.Marshal(alert)
	resp, err := http.Post(a.config.WebhookURL, "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (a *Alerter) sendEmail(alert Alert) error {
	subject := fmt.Sprintf("PoolForge [%s] %s", alert.Level, alert.Message)
	body := fmt.Sprintf("Pool: %s\nDevice: %s\nLevel: %s\nMessage: %s\nTime: %s",
		alert.Pool, alert.Device, alert.Level, alert.Message, alert.Timestamp.Format(time.RFC3339))
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\n\r\n%s",
		a.config.SMTPFrom, strings.Join(a.config.SMTPTo, ","), subject, body)

	addr := fmt.Sprintf("%s:%d", a.config.SMTPHost, a.config.SMTPPort)
	var auth smtp.Auth
	if a.config.SMTPUser != "" {
		auth = smtp.PlainAuth("", a.config.SMTPUser, a.config.SMTPPass, a.config.SMTPHost)
	}
	return smtp.SendMail(addr, auth, a.config.SMTPFrom, a.config.SMTPTo, []byte(msg))
}
