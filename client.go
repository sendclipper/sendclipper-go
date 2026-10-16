package sendclipper

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	mrand "math/rand/v2"
	"mime"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is where requests go unless WithBaseURL or SENDCLIPPER_BASE_URL says otherwise.
const DefaultBaseURL = "https://sendclipper.com"

const maxRetryAfter = 60 * time.Second

// Client talks to the SendClipper API. Make one with [NewClient] and share
// it: it is safe for concurrent use.
type Client struct {
	Emails       *EmailsService
	Batch        *BatchService
	Domains      *DomainsService
	Templates    *TemplatesService
	Sandboxes    *SandboxesService
	Webhooks     *WebhooksService
	Contacts     *ContactsService
	Audiences    *AudiencesService
	Suppressions *SuppressionsService
	Mailboxes    *MailboxesService
	Automations  *AutomationsService
	Workspace    *WorkspaceService
	APIKeys      *APIKeysService

	apiKey         string
	baseURL        string
	http           *http.Client
	timeout        time.Duration
	maxRetries     int
	defaultHeaders http.Header
	sleep          func(ctx context.Context, d time.Duration) error
}

// NewClient makes a client. An empty apiKey reads SENDCLIPPER_API_KEY.
func NewClient(apiKey string, opts ...ClientOption) *Client {
	if apiKey == "" {
		apiKey = os.Getenv("SENDCLIPPER_API_KEY")
	}
	base := os.Getenv("SENDCLIPPER_BASE_URL")
	if base == "" {
		base = DefaultBaseURL
	}
	c := &Client{
		apiKey:         apiKey,
		baseURL:        normaliseBaseURL(base),
		http:           &http.Client{CheckRedirect: sameHostRedirects},
		timeout:        30 * time.Second,
		maxRetries:     2,
		defaultHeaders: http.Header{},
		sleep:          sleepCtx,
	}
	for _, o := range opts {
		o(c)
	}
	c.Emails = &EmailsService{c}
	c.Batch = &BatchService{c}
	c.Domains = &DomainsService{c}
	c.Templates = &TemplatesService{c: c, Tags: &TemplateTagsService{c}, Assets: &TemplateAssetsService{c}}
	c.Sandboxes = &SandboxesService{c: c, Messages: &SandboxMessagesService{c}}
	c.Webhooks = &WebhooksService{c: c, Dispatches: &WebhookDispatchesService{c}}
	c.Contacts = &ContactsService{c}
	c.Audiences = &AudiencesService{c}
	c.Suppressions = &SuppressionsService{c}
	c.Mailboxes = &MailboxesService{c: c, Messages: &MailboxMessagesService{c}}
	c.Automations = &AutomationsService{c}
	c.Workspace = &WorkspaceService{c}
	c.APIKeys = &APIKeysService{c}
	return c
}

// BaseURL is the API origin requests go to, without /v1.
func (c *Client) BaseURL() string { return c.baseURL }

func normaliseBaseURL(u string) string {
	u = strings.TrimRight(u, "/")
	return strings.TrimSuffix(u, "/v1")
}

// sameHostRedirects follows redirects, but never carries the API key to another host.
func sameHostRedirects(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	if req.URL.Host != via[0].URL.Host {
		req.Header.Del("Authorization")
	}
	return nil
}

type request struct {
	method   string
	path     string
	query    url.Values
	body     any    // JSON-encoded when non-nil
	form     []byte // a multipart body; formType is its Content-Type
	formType string
	as       string // "json" (default), "text" or "bytes"
	bearer   string // replaces the API key (the sandbox JSON door)
	out      any    // where a JSON answer is decoded to
}

// do sends one logical call, with retries, and decodes the answer into req.out.
func (c *Client) do(ctx context.Context, req request, opts []RequestOption) error {
	cfg := requestConfig{}
	for _, o := range opts {
		o(&cfg)
	}
	token := req.bearer
	if token == "" {
		token = c.apiKey
	}
	if token == "" {
		return &Error{
			Name:       "api_key_missing",
			Message:    `No SendClipper API key. Pass one to sendclipper.NewClient("sc_live_...") or set SENDCLIPPER_API_KEY.`,
			StatusCode: 401,
		}
	}

	u := c.baseURL + "/v1" + req.path
	if q := req.query.Encode(); q != "" {
		u += "?" + q
	}

	var payload []byte
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+token)
	if req.as == "" || req.as == "json" {
		headers.Set("Accept", "application/json")
	} else {
		headers.Set("Accept", "*/*")
	}
	headers.Set("User-Agent", "sendclipper-go/"+Version)
	for k, v := range c.defaultHeaders {
		headers[k] = v
	}
	for k, v := range cfg.headers {
		headers[k] = v
	}
	switch {
	case req.form != nil:
		payload = req.form
		headers.Set("Content-Type", req.formType)
	case req.body != nil:
		// No HTML escaping: an email body travels as written, not as \u003cp\u003e.
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(req.body); err != nil {
			return fmt.Errorf("sendclipper: encoding the request: %w", err)
		}
		payload = bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
		headers.Set("Content-Type", "application/json")
	}

	isRead := req.method == http.MethodGet || req.method == http.MethodHead
	// The API de-duplicates every JSON mutation on this header for 24 hours.
	// A multipart body is not fingerprinted server-side, so it gets no key
	// and is never retried.
	if !isRead && req.form == nil {
		key := cfg.idempotencyKey
		if key == "" {
			key = "sc-go-" + uuid4()
		}
		headers.Set("Idempotency-Key", key)
	} else if cfg.idempotencyKey != "" {
		headers.Set("Idempotency-Key", cfg.idempotencyKey)
	}
	retries := 0
	if isRead || headers.Get("Idempotency-Key") != "" {
		retries = c.maxRetries
		if cfg.maxRetries != nil {
			retries = *cfg.maxRetries
		}
	}
	timeout := c.timeout
	if cfg.timeout > 0 {
		timeout = cfg.timeout
	}

	for attempt := 0; ; attempt++ {
		status, resHeaders, body, err := c.once(ctx, req.method, u, headers, payload, timeout)
		if err != nil {
			if ctx.Err() != nil {
				return &Error{Name: "network_error", Message: "The request was cancelled: " + ctx.Err().Error(), cause: ctx.Err()}
			}
			if attempt < retries {
				if serr := c.sleep(ctx, backoff(attempt)); serr != nil {
					return &Error{Name: "network_error", Message: "The request was cancelled: " + serr.Error(), cause: serr}
				}
				continue
			}
			return connectionError(err, req.method, u, c.baseURL, timeout)
		}

		ok := status >= 200 && status < 300
		if !ok && retryableStatus(status) && attempt < retries {
			if serr := c.sleep(ctx, waitFor(resHeaders, attempt)); serr != nil {
				return &Error{Name: "network_error", Message: "The request was cancelled: " + serr.Error(), cause: serr}
			}
			continue
		}
		if !ok {
			apiErr := toAPIError(status, body, resHeaders)
			// Our own earlier attempt is still running server-side (a timeout
			// that was not a failure). Its result is replayed once it finishes.
			if apiErr.Name == "idempotency_key_in_flight" && attempt < retries {
				if serr := c.sleep(ctx, waitFor(resHeaders, attempt)); serr != nil {
					return &Error{Name: "network_error", Message: "The request was cancelled: " + serr.Error(), cause: serr}
				}
				continue
			}
			return apiErr
		}

		if cfg.responseHeaders != nil {
			*cfg.responseHeaders = resHeaders
		}
		return decode(req, resHeaders, body)
	}
}

func (c *Client) once(ctx context.Context, method, u string, headers http.Header, payload []byte, timeout time.Duration) (int, http.Header, []byte, error) {
	actx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	hreq, err := http.NewRequestWithContext(actx, method, u, body)
	if err != nil {
		return 0, nil, nil, err
	}
	hreq.Header = headers.Clone()
	res, err := c.http.Do(hreq)
	if err != nil {
		return 0, nil, nil, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return 0, nil, nil, err
	}
	return res.StatusCode, res.Header, data, nil
}

func decode(req request, headers http.Header, body []byte) error {
	switch req.as {
	case "text":
		if p, ok := req.out.(*string); ok {
			*p = string(body)
		}
		return nil
	case "bytes":
		if p, ok := req.out.(*DownloadedFile); ok {
			*p = DownloadedFile{Content: body, ContentType: headers.Get("Content-Type"), Filename: filenameOf(headers.Get("Content-Disposition"))}
		}
		return nil
	}
	if req.out == nil || len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, req.out); err != nil {
		return &Error{Name: "invalid_response", Message: "SendClipper's answer could not be decoded: " + err.Error(), StatusCode: 200, Headers: headers, cause: err}
	}
	return nil
}

func connectionError(err error, method, u, base string, timeout time.Duration) *Error {
	path := u
	if pu, perr := url.Parse(u); perr == nil {
		path = pu.Path
	}
	if errors.Is(err, context.DeadlineExceeded) || isTimeout(err) {
		return &Error{Name: "network_error", Message: fmt.Sprintf("SendClipper did not answer within %s (%s %s).", timeout, method, path), cause: err}
	}
	return &Error{Name: "network_error", Message: fmt.Sprintf("Could not reach SendClipper at %s: %v", base, err), cause: err}
}

func isTimeout(err error) bool {
	var t interface{ Timeout() bool }
	return errors.As(err, &t) && t.Timeout()
}

func retryableStatus(s int) bool {
	switch s {
	case 408, 429, 500, 502, 503, 504:
		return true
	}
	return false
}

func backoff(attempt int) time.Duration {
	base := math.Min(8, 0.5*math.Pow(2, float64(attempt)))
	return time.Duration((base/2 + mrand.Float64()*(base/2)) * float64(time.Second))
}

func waitFor(h http.Header, attempt int) time.Duration {
	if d, ok := retryAfter(h); ok {
		return d
	}
	return backoff(attempt)
}

func retryAfter(h http.Header) (time.Duration, bool) {
	raw := h.Get("Retry-After")
	if raw == "" {
		return 0, false
	}
	if secs, err := strconv.ParseFloat(raw, 64); err == nil && secs >= 0 {
		return min(maxRetryAfter, time.Duration(secs*float64(time.Second))), true
	}
	if at, err := http.ParseTime(raw); err == nil {
		return min(maxRetryAfter, max(0, time.Until(at))), true
	}
	return 0, false
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func filenameOf(disposition string) string {
	if disposition == "" {
		return ""
	}
	_, params, err := mime.ParseMediaType(disposition)
	if err == nil && params["filename"] != "" {
		return params["filename"] // mime decodes filename*=UTF-8''...
	}
	return ""
}

func uuid4() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// seg percent-encodes one path segment.
func seg(s string) string { return url.PathEscape(s) }

// query builds url.Values, dropping empty values. Booleans are written
// the way the API reads them ("true"/"false").
type query url.Values

func q() query { return query{} }

func (v query) str(k, s string) query {
	if s != "" {
		v[k] = []string{s}
	}
	return v
}

func (v query) num(k string, n int) query {
	if n != 0 {
		v[k] = []string{strconv.Itoa(n)}
	}
	return v
}

func (v query) flag(k string, b *bool) query {
	if b != nil {
		v[k] = []string{strconv.FormatBool(*b)}
	}
	return v
}

func (v query) values() url.Values { return url.Values(v) }
