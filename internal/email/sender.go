package email

import "log/slog"

func NewSender(apiKey, from string, logger *slog.Logger) Sender {
	if apiKey == "" {
		return NewDevSink(logger)
	}
	return NewResendSender(apiKey, from)
}
