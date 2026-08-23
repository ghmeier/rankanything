package email

import "log/slog"

// NewSender picks a Sender based on whether a Resend API key is configured.
// An empty key means local development or CI, where there's no key and no
// intent to actually deliver mail, so it returns the dev log sink instead of
// building an adapter that would only fail against Resend's API.
func NewSender(apiKey, from string, logger *slog.Logger) Sender {
	if apiKey == "" {
		return NewDevSink(logger)
	}
	return NewResendSender(apiKey, from)
}
