package sendclipper

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// WebhookTolerance is SendClipper's documented tolerance: five minutes.
const WebhookTolerance = 5 * time.Minute

// SignatureHeader is the header SendClipper signs deliveries in.
const SignatureHeader = "X-SendClipper-Signature-V2"

// WebhookVerificationError is why [VerifyWebhook] refused a delivery.
type WebhookVerificationError struct {
	// Reason is missing_header, malformed_header, timestamp_out_of_tolerance,
	// signature_mismatch or invalid_payload.
	Reason  string
	Message string
}

func (e *WebhookVerificationError) Error() string {
	return "sendclipper: webhook " + e.Reason + ": " + e.Message
}

// VerifyOption tunes [VerifyWebhook].
type VerifyOption func(*verifyConfig)

type verifyConfig struct {
	tolerance time.Duration
	now       time.Time
}

// WithTolerance replaces the five-minute tolerance.
func WithTolerance(d time.Duration) VerifyOption { return func(c *verifyConfig) { c.tolerance = d } }

// WithNow sets the current time, for tests.
func WithNow(t time.Time) VerifyOption { return func(c *verifyConfig) { c.now = t } }

// VerifyWebhook verifies a delivery and returns its parsed event.
//
// payload is the body EXACTLY as received (read r.Body with io.ReadAll
// before decoding it), never re-encoded JSON; headers is r.Header.
//
// SendClipper signs "{timestamp}.{rawBody}" with HMAC-SHA256 and sends
// X-SendClipper-Signature-V2: t=<seconds>,v1=<hex>[,v1=<hex>]. During a
// secret rotation there are two v1= values; any match is accepted.
func VerifyWebhook(payload []byte, headers http.Header, secret string, opts ...VerifyOption) (*WebhookEvent, error) {
	return VerifyWebhookSignature(payload, strings.Join(headers.Values(SignatureHeader), ","), secret, opts...)
}

// VerifyWebhookSignature is [VerifyWebhook] for when you hold only the
// X-SendClipper-Signature-V2 header's value.
func VerifyWebhookSignature(payload []byte, signature, secret string, opts ...VerifyOption) (*WebhookEvent, error) {
	cfg := verifyConfig{tolerance: WebhookTolerance, now: time.Now()}
	for _, o := range opts {
		o(&cfg)
	}
	if secret == "" {
		return nil, &WebhookVerificationError{"missing_header", "No webhook signing secret was given."}
	}
	if signature == "" {
		return nil, &WebhookVerificationError{"missing_header", "The request carries no X-SendClipper-Signature-V2 header."}
	}
	timestamp, signatures, ok := ParseSignatureHeader(signature)
	if !ok || len(signatures) == 0 {
		return nil, &WebhookVerificationError{"malformed_header", "X-SendClipper-Signature-V2 has no t= or no v1= value."}
	}

	drift := cfg.now.Unix() - timestamp
	if drift < 0 {
		drift = -drift
	}
	if time.Duration(drift)*time.Second > cfg.tolerance {
		return nil, &WebhookVerificationError{"timestamp_out_of_tolerance",
			fmt.Sprintf("The delivery was signed %d s from now, outside the %d s tolerance.", drift, int64(cfg.tolerance/time.Second))}
	}

	expected := []byte(SignPayload(secret, timestamp, payload))
	// ANY match, and every candidate is compared: a rotation sends two.
	matched := false
	for _, candidate := range signatures {
		if hmac.Equal(expected, []byte(candidate)) {
			matched = true
		}
	}
	if !matched {
		return nil, &WebhookVerificationError{"signature_mismatch", "The signature does not match this secret and body."}
	}

	event := new(WebhookEvent)
	if err := json.Unmarshal(payload, event); err != nil {
		return nil, &WebhookVerificationError{"invalid_payload", "The signed body is not a webhook event: " + err.Error()}
	}
	return event, nil
}

// SignPayload is hex(HMAC-SHA256(secret, "{timestamp}.{rawBody}")). Exported for tests of your own handler.
func SignPayload(secret string, timestamp int64, rawBody []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(timestamp, 10) + "."))
	mac.Write(rawBody)
	return hex.EncodeToString(mac.Sum(nil))
}

// WebhookSignatureHeader builds a valid header value for a body, e.g. to
// test your endpoint locally. A zero timestamp means now.
func WebhookSignatureHeader(secret string, rawBody []byte, timestamp int64) string {
	if timestamp == 0 {
		timestamp = time.Now().Unix()
	}
	return fmt.Sprintf("t=%d,v1=%s", timestamp, SignPayload(secret, timestamp, rawBody))
}

// ParseSignatureHeader reads t=<seconds>,v1=<hex>[,v1=<hex>]. ok is false when there is no valid t=.
func ParseSignatureHeader(header string) (timestamp int64, signatures []string, ok bool) {
	for _, part := range strings.Split(header, ",") {
		key, value, found := strings.Cut(part, "=")
		if !found {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		switch {
		case key == "t" && value != "" && strings.Trim(value, "0123456789") == "":
			if n, err := strconv.ParseInt(value, 10, 64); err == nil {
				timestamp, ok = n, true
			}
		case key == "v1" && value != "":
			signatures = append(signatures, value)
		}
	}
	return timestamp, signatures, ok
}
