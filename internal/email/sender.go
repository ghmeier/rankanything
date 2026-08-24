package email

import "log/slog"

// NewSender falls back to the dev sink when no API key is set, since that
// means local development or CI, where mail is not meant to leave.
func NewSender(apiKey, from string, logger *slog.Logger) Sender {
	if apiKey == "" {
		return NewDevSink(logger)
	}
	return NewResendSender(apiKey, from)
}
