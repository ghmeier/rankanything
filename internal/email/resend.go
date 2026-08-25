package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const resendAPIBaseURL = "https://api.resend.com"

type ResendSender struct {
	apiKey     string
	from       string
	baseURL    string
	httpClient *http.Client
}

func NewResendSender(apiKey, from string) *ResendSender {
	return newResendSender(apiKey, from, resendAPIBaseURL)
}

func newResendSender(apiKey, from, baseURL string) *ResendSender {
	return &ResendSender{
		apiKey:     apiKey,
		from:       from,
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

type resendRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	HTML    string   `json:"html"`
	Text    string   `json:"text"`
}

func (s *ResendSender) Send(ctx context.Context, msg Message) error {
	body, err := json.Marshal(resendRequest{
		From:    s.from,
		To:      []string{msg.To},
		Subject: msg.Subject,
		HTML:    msg.HTML,
		Text:    msg.Text,
	})
	if err != nil {
		return fmt.Errorf("email: encode resend request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/emails", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("email: build resend request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("email: send via resend: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("email: resend returned %d: %s", resp.StatusCode, string(b))
	}
	return nil
}
