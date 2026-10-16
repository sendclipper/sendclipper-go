package sendclipper

import (
	"net/http"
	"time"
)

// ClientOption configures a [Client].
type ClientOption func(*Client)

// WithBaseURL points the client at another API origin (no /v1). The default
// is SENDCLIPPER_BASE_URL, then https://sendclipper.com.
func WithBaseURL(url string) ClientOption {
	return func(c *Client) { c.baseURL = normaliseBaseURL(url) }
}

// WithHTTPClient sends requests through your own *http.Client (a proxy, a
// custom transport). Its Timeout, if any, applies on top of the per-attempt one.
func WithHTTPClient(hc *http.Client) ClientOption { return func(c *Client) { c.http = hc } }

// WithTimeout sets the per-attempt timeout. Default 30 seconds.
func WithTimeout(d time.Duration) ClientOption { return func(c *Client) { c.timeout = d } }

// WithMaxRetries sets how often a network error, 429 or 5xx is retried. Default 2.
func WithMaxRetries(n int) ClientOption {
	return func(c *Client) { c.maxRetries = max(0, n) }
}

// WithDefaultHeader adds a header to every request.
func WithDefaultHeader(name, value string) ClientOption {
	return func(c *Client) { c.defaultHeaders.Set(name, value) }
}

// RequestOption adjusts one call.
type RequestOption func(*requestConfig)

type requestConfig struct {
	idempotencyKey  string
	timeout         time.Duration
	maxRetries      *int
	headers         http.Header
	responseHeaders *http.Header
}

// WithIdempotencyKey sets your own Idempotency-Key, so that YOUR retries of
// the same logical operation (a job that runs again after a crash) are safe
// too. Without it every write still gets a fresh key of its own.
func WithIdempotencyKey(key string) RequestOption {
	return func(r *requestConfig) { r.idempotencyKey = key }
}

// WithRequestTimeout overrides the client's per-attempt timeout for one call.
func WithRequestTimeout(d time.Duration) RequestOption {
	return func(r *requestConfig) { r.timeout = d }
}

// WithRequestMaxRetries overrides the client's retry count for one call.
func WithRequestMaxRetries(n int) RequestOption {
	return func(r *requestConfig) { n = max(0, n); r.maxRetries = &n }
}

// WithHeader adds a header to one call.
func WithHeader(name, value string) RequestOption {
	return func(r *requestConfig) {
		if r.headers == nil {
			r.headers = http.Header{}
		}
		r.headers.Set(name, value)
	}
}

// WithResponseHeaders stores the response's headers in *dst when the call
// succeeds (on failure they are on the [*Error]).
func WithResponseHeaders(dst *http.Header) RequestOption {
	return func(r *requestConfig) { r.responseHeaders = dst }
}

// Bool returns a pointer to b, for the optional booleans in params
// (TrackOpens: sendclipper.Bool(false)).
func Bool(b bool) *bool { return &b }
