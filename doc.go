// Package sendclipper is the official Go client for the SendClipper email API.
//
//	client := sendclipper.NewClient("sc_live_...") // or NewClient(""): reads SENDCLIPPER_API_KEY
//	email, err := client.Emails.Send(ctx, &sendclipper.SendEmailParams{
//		From:    "hello@send.acme.io",
//		To:      "ada@lumen.io",
//		Subject: "Your receipt",
//		HTML:    "<p>Thanks for your order.</p>",
//	})
//	var apiErr *sendclipper.Error
//	if errors.As(err, &apiErr) {
//		log.Println(apiErr.Name, apiErr.Message) // e.g. domain_not_verified
//	}
//
// Every method takes a context and returns the decoded response or an
// error. A refusal from the API or a failed connection is always a
// [*Error] whose Name is SendClipper's machine-readable code
// ("validation_failed", "rate_limited", "network_error", ...).
//
// Network errors, 429 and 5xx answers are retried with backoff, honouring
// Retry-After. Every JSON write carries an Idempotency-Key, and the API
// answers a repeated key with the first result for 24 hours, so a retried
// send never goes out twice.
//
// Verify webhook deliveries with [VerifyWebhook].
package sendclipper
