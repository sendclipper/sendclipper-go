package sendclipper

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

var sent = map[string]any{
	"id": "0b6c0000-0000-4000-8000-000000000001", "messageId": "msg_9f2b7c10ad", "sesMessageId": nil,
	"from": "hello@acme.io", "to": "ada@lumen.io", "domain": "acme.io", "subject": "Hi",
	"status": "queued", "createdAt": "2026-10-16T10:00:00.000Z", "templateId": nil,
}

var msg = &SendEmailParams{From: "a@acme.io", To: "b@lumen.io", Subject: "s", HTML: "h"}

var ctx = context.Background()

func TestSendPostsToV1EmailsWithTheKeyAndAnIdempotencyKey(t *testing.T) {
	c, f := newFake(t, reply{Status: 201, Body: sent, Headers: map[string]string{"X-Request-Id": "r1"}})
	var h http.Header
	email, err := c.Emails.Send(ctx, &SendEmailParams{From: "hello@acme.io", To: "ada@lumen.io", Subject: "Hi", HTML: "<p>Hi</p>"}, WithResponseHeaders(&h))
	if err != nil {
		t.Fatal(err)
	}
	if email.ID != sent["id"] || email.Status != StatusQueued || email.CreatedAt.Year() != 2026 || email.TemplateID != nil {
		t.Fatalf("decoded %+v", email)
	}
	if h.Get("X-Request-Id") != "r1" {
		t.Fatalf("response headers not captured: %v", h)
	}
	call := f.Calls()[0]
	if call.Method != "POST" || call.Path != "/v1/emails" {
		t.Fatalf("%s %s", call.Method, call.Path)
	}
	if call.Header.Get("Authorization") != "Bearer sc_live_test" || call.Header.Get("Content-Type") != "application/json" ||
		call.Header.Get("User-Agent") != "sendclipper-go/"+Version {
		t.Fatalf("headers %v", call.Header)
	}
	if !regexp.MustCompile(`^sc-go-[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(call.Header.Get("Idempotency-Key")) {
		t.Fatalf("idempotency key %q", call.Header.Get("Idempotency-Key"))
	}
	if string(call.Body) != `{"from":"hello@acme.io","to":"ada@lumen.io","subject":"Hi","html":"<p>Hi</p>"}` {
		t.Fatalf("body %s", call.Body)
	}
}

func TestATemplateSendCarriesTemplateIDVariablesAndACallerKey(t *testing.T) {
	c, f := newFake(t, reply{Status: 201, Body: sent})
	_, err := c.Emails.Send(ctx, &SendEmailParams{From: "hello@acme.io", To: "ada@lumen.io", TemplateID: "tpl_9f2b7c10ad42",
		Variables: map[string]any{"first_name": "Ada"}, TrackOpens: Bool(false)}, WithIdempotencyKey("order-42-receipt"))
	if err != nil {
		t.Fatal(err)
	}
	if f.Calls()[0].Header.Get("Idempotency-Key") != "order-42-receipt" {
		t.Fatal("caller key not used")
	}
	want := `{"from":"hello@acme.io","to":"ada@lumen.io","templateId":"tpl_9f2b7c10ad42","variables":{"first_name":"Ada"},"trackOpens":false}`
	if string(f.Calls()[0].Body) != want {
		t.Fatalf("body %s", f.Calls()[0].Body)
	}
}

// replyToAndFile is a send carrying two Reply-To addresses and one file.
var replyToAndFile = &SendEmailParams{From: "bookings@acme.io", To: "ada@lumen.io", Subject: "Your booking", HTML: "<p>Attached.</p>",
	ReplyTo:     []string{"support@acme.io", "ops@acme.io"},
	Attachments: []Attachment{{Filename: "Q&A booking-42.pdf", Content: []byte("%PDF-1.7\n\xff\xfe"), ContentType: "application/pdf"}}}

const replyToAndFileBody = `{"from":"bookings@acme.io","to":"ada@lumen.io","subject":"Your booking","html":"<p>Attached.</p>",` +
	`"replyTo":["support@acme.io","ops@acme.io"],` +
	`"attachments":[{"filename":"Q&A booking-42.pdf","content":"JVBERi0xLjcK//4=","contentType":"application/pdf"}]}`

func TestASendCarriesReplyToAndBase64EncodedAttachments(t *testing.T) {
	c, f := newFake(t, reply{Status: 201, Body: sent})
	if _, err := c.Emails.Send(ctx, replyToAndFile); err != nil {
		t.Fatal(err)
	}
	if got := string(f.Calls()[0].Body); got != replyToAndFileBody {
		t.Fatalf("body %s\nwant %s", got, replyToAndFileBody)
	}
}

func TestTheSandboxSendCarriesReplyToAndAttachmentsToo(t *testing.T) {
	c, f := newFake(t, reply{Status: 201, Body: map[string]any{"id": "sbx-msg"}})
	if _, err := c.Sandboxes.Send(ctx, "sc_sbx_abc", replyToAndFile); err != nil {
		t.Fatal(err)
	}
	if call := f.Calls()[0]; call.Path != "/v1/sandbox/emails" || string(call.Body) != replyToAndFileBody {
		t.Fatalf("%s %s", call.Path, call.Body)
	}
}

func TestAnAttachmentWithNoContentTypeLeavesItOutAndABatchItemCarriesReplyTo(t *testing.T) {
	b, err := json.Marshal(Attachment{Filename: "a.txt", Content: []byte("hi?")})
	if err != nil || string(b) != `{"filename":"a.txt","content":"aGk/"}` {
		t.Fatalf("%s %v", b, err)
	}
	c, f := newFake(t, ok(map[string]any{"data": []any{}, "summary": map[string]any{}}))
	_, _ = c.Batch.Send(ctx, []SendEmailParams{{From: "a@acme.io", To: "b@lumen.io", Subject: "s", HTML: "h", ReplyTo: []string{"support@acme.io"}}})
	if want := `{"emails":[{"from":"a@acme.io","to":"b@lumen.io","subject":"s","html":"h","replyTo":["support@acme.io"]}]}`; string(f.Calls()[0].Body) != want {
		t.Fatalf("%s", f.Calls()[0].Body)
	}
}

func TestEmptyParamsTravelAsJSONObjects(t *testing.T) {
	c, f := newFake(t, ok(map[string]any{}), ok(map[string]any{}), ok(map[string]any{}))
	_, _ = c.Templates.Create(ctx, nil)
	_, _ = c.Templates.Preview(ctx, "t1", nil)
	_, _ = c.Webhooks.Dispatches.ReplayAll(ctx, nil)
	for i, want := range []string{`{}`, `{"variables":{}}`, `{}`} {
		if string(f.Calls()[i].Body) != want {
			t.Fatalf("call %d body %s, want %s", i, f.Calls()[i].Body, want)
		}
	}
}

func TestA503IsRetriedWithTheSameIdempotencyKey(t *testing.T) {
	c, f := newFake(t, apiErr(503, "unavailable", "down"), reply{Status: 201, Body: sent})
	if _, err := c.Emails.Send(ctx, msg); err != nil {
		t.Fatal(err)
	}
	if len(f.Calls()) != 2 || f.Calls()[0].Header.Get("Idempotency-Key") != f.Calls()[1].Header.Get("Idempotency-Key") {
		t.Fatalf("calls %d, keys %q %q", len(f.Calls()), f.Calls()[0].Header.Get("Idempotency-Key"), f.Calls()[1].Header.Get("Idempotency-Key"))
	}
}

func TestANetworkErrorIsRetriedThenAnswersNetworkErrorWithNoStatus(t *testing.T) {
	c, f := newFake(t, reply{Drop: true}, reply{Drop: true})
	c.maxRetries = 1
	_, err := c.Emails.Get(ctx, "x")
	e := asError(t, err)
	if e.Name != "network_error" || e.StatusCode != 0 || !strings.Contains(e.Message, "Could not reach SendClipper at "+f.srv.URL) {
		t.Fatalf("%+v", e)
	}
	if errors.Unwrap(e) == nil {
		t.Fatal("network_error should wrap the transport error")
	}
	if len(f.Calls()) != 2 {
		t.Fatalf("calls %d", len(f.Calls()))
	}
}

func TestATimeoutNamesTheLimitAndIsRetried(t *testing.T) {
	c, f := newFake(t, reply{Delay: time.Second, Body: map[string]any{}}, reply{Delay: time.Second, Body: map[string]any{}})
	_, err := c.Workspace.Get(ctx, WithRequestTimeout(50*time.Millisecond), WithRequestMaxRetries(1))
	e := asError(t, err)
	if e.Name != "network_error" || !strings.Contains(e.Message, "within 50ms") {
		t.Fatalf("%+v", e)
	}
	if len(f.Calls()) != 2 {
		t.Fatalf("calls %d", len(f.Calls()))
	}
}

func TestACancelledContextStopsAtOnce(t *testing.T) {
	c, f := newFake(t, reply{Delay: time.Second, Body: map[string]any{}})
	cctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	_, err := c.Domains.List(cctx)
	e := asError(t, err)
	if e.Name != "network_error" || !errors.Is(err, context.DeadlineExceeded) || len(f.Calls()) != 1 {
		t.Fatalf("%+v (calls %d)", e, len(f.Calls()))
	}
}

func Test429HonoursRetryAfterCappedAtAMinute(t *testing.T) {
	c, f := newFake(t,
		reply{Status: 429, Body: map[string]any{"error": map[string]any{"code": "rate_limited", "message": "slow"}}, Headers: map[string]string{"Retry-After": "3"}},
		reply{Status: 503, Headers: map[string]string{"Retry-After": "86400"}},
		ok([]any{}))
	if _, err := c.Domains.List(ctx, WithRequestMaxRetries(2)); err != nil {
		t.Fatal(err)
	}
	if len(f.Slept()) != 2 || f.Slept()[0] != 3*time.Second || f.Slept()[1] != time.Minute {
		t.Fatalf("slept %v", f.Slept())
	}
}

func TestBackoffGrowsAndStaysBounded(t *testing.T) {
	for a := 0; a < 10; a++ {
		b := backoff(a).Seconds()
		base := min(8.0, 0.5*float64(int(1)<<a))
		if b < base/2 || b > base {
			t.Fatalf("attempt %d: %v outside [%v, %v]", a, b, base/2, base)
		}
	}
}

func TestIdempotencyKeyInFlightIsRetriedAndAnyOther409IsNot(t *testing.T) {
	inFlight := apiErr(409, "idempotency_key_in_flight", "wait")
	inFlight.Headers = map[string]string{"Retry-After": "0"}
	c, f := newFake(t, inFlight, reply{Status: 201, Body: sent})
	if _, err := c.Emails.Send(ctx, msg); err != nil || len(f.Calls()) != 2 {
		t.Fatalf("err %v calls %d", err, len(f.Calls()))
	}
	c, f = newFake(t, apiErr(409, "sandbox_name_taken", "taken"))
	_, err := c.Sandboxes.Create(ctx, &SandboxParams{Name: "x"})
	e := asError(t, err)
	if e.Name != "sandbox_name_taken" || e.Message != "taken" || e.StatusCode != 409 || len(f.Calls()) != 1 {
		t.Fatalf("%+v calls %d", e, len(f.Calls()))
	}
}

func TestA4xxIsNotRetriedAndTheEnvelopeBecomesATypedError(t *testing.T) {
	c, f := newFake(t, reply{Status: 400, Body: map[string]any{"error": map[string]any{
		"code": "template_variables_missing", "message": "Missing first_name", "detail": map[string]any{"missing": []string{"first_name"}}}}})
	_, err := c.Emails.Send(ctx, &SendEmailParams{From: "a@acme.io", To: "b@lumen.io", TemplateID: "tpl_x"})
	e := asError(t, err)
	if e.Name != "template_variables_missing" || e.Message != "Missing first_name" || e.StatusCode != 400 ||
		string(e.Detail) != `{"missing":["first_name"]}` || len(f.Calls()) != 1 {
		t.Fatalf("%+v %s", e, e.Detail)
	}
	if e.Error() != "sendclipper: template_variables_missing (400): Missing first_name" {
		t.Fatalf("Error() = %q", e.Error())
	}
	var target *Error
	if !errors.As(err, &target) {
		t.Fatal("errors.As")
	}
}

func TestAValidationErrorInNestsDefaultShapeBecomesValidationFailed(t *testing.T) {
	c, _ := newFake(t, reply{Status: 400, Body: map[string]any{"statusCode": 400, "message": []string{"to must be a valid email address"}, "error": "Bad Request"}})
	_, err := c.Emails.Send(ctx, &SendEmailParams{From: "a@acme.io", To: "nope", Subject: "s", HTML: "h"})
	e := asError(t, err)
	if e.Name != "validation_failed" || e.Message != "to must be a valid email address" {
		t.Fatalf("%+v", e)
	}
}

func TestANonJSONErrorBodyStillAnswersACode(t *testing.T) {
	c, _ := newFake(t, reply{Status: 502, Body: "<html>Bad gateway</html>"})
	c.maxRetries = 0
	_, err := c.Domains.List(ctx)
	e := asError(t, err)
	if e.Name != "http_502" || e.Message != "<html>Bad gateway</html>" {
		t.Fatalf("%+v", e)
	}
}

func TestGetRequestsCarryNoIdempotencyKeyAndDropEmptyQueryValues(t *testing.T) {
	c, f := newFake(t, ok(map[string]any{"items": []any{}, "total": 0}))
	_, _ = c.Emails.List(ctx, &ListEmailsParams{Status: StatusBounced, PageSize: 50})
	if f.Calls()[0].Header.Get("Idempotency-Key") != "" {
		t.Fatal("GET carried an idempotency key")
	}
	if len(f.Calls()[0].Query) != 2 || f.Calls()[0].Query["status"] != "bounced" || f.Calls()[0].Query["pageSize"] != "50" {
		t.Fatalf("query %v", f.Calls()[0].Query)
	}
}

func TestBooleansInTheQueryAreWrittenTheWayTheAPIReadsThem(t *testing.T) {
	c, f := newFake(t, ok([]any{}), ok([]any{}))
	_, _ = c.Mailboxes.List(ctx, true)
	_, _ = c.Webhooks.Deliveries(ctx, &ListWebhookDeliveriesParams{FinalOnly: Bool(false)})
	if f.Calls()[0].Query["includeArchived"] != "true" || f.Calls()[1].Query["finalOnly"] != "false" {
		t.Fatalf("%v %v", f.Calls()[0].Query, f.Calls()[1].Query)
	}
}

func TestEmailsAllWalksEveryPageAndStopsOnTheLast(t *testing.T) {
	row := func(id string) map[string]any {
		r := map[string]any{}
		for k, v := range sent {
			r[k] = v
		}
		r["id"] = id
		return r
	}
	c, f := newFake(t,
		ok(map[string]any{"items": []any{row("id-1"), row("id-2")}, "total": 3}),
		ok(map[string]any{"items": []any{row("id-3")}, "total": 3}))
	var ids []string
	for e, err := range c.Emails.All(ctx, &ListEmailsParams{PageSize: 2}) {
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, e.ID)
	}
	if strings.Join(ids, ",") != "id-1,id-2,id-3" || len(f.Calls()) != 2 || f.Calls()[1].Query["page"] != "2" {
		t.Fatalf("ids %v calls %d", ids, len(f.Calls()))
	}
}

func TestBreakingOutOfAnIteratorStopsFetching(t *testing.T) {
	c, f := newFake(t, ok(map[string]any{"data": []any{map[string]any{"id": "a"}, map[string]any{"id": "b"}}, "page": map[string]any{"cursor": "c1", "hasMore": true}}))
	for range c.Suppressions.All(ctx, nil) {
		break
	}
	if len(f.Calls()) != 1 {
		t.Fatalf("calls %d", len(f.Calls()))
	}
}

func TestSuppressionsAllFollowsTheCursor(t *testing.T) {
	s := func(email string) map[string]any {
		return map[string]any{"id": email, "email": email, "reason": "manual"}
	}
	c, f := newFake(t,
		ok(map[string]any{"data": []any{s("a@x.io")}, "page": map[string]any{"cursor": "c1", "hasMore": true}}),
		ok(map[string]any{"data": []any{s("b@x.io")}, "page": map[string]any{"cursor": nil, "hasMore": false}}))
	var seen []string
	for r, err := range c.Suppressions.All(ctx, &ListSuppressionsParams{Reason: "manual", Cursor: "ignored"}) {
		if err != nil {
			t.Fatal(err)
		}
		seen = append(seen, r.Email)
	}
	if strings.Join(seen, ",") != "a@x.io,b@x.io" {
		t.Fatalf("%v", seen)
	}
	if _, has := f.Calls()[0].Query["cursor"]; has {
		t.Fatal("the first page must not carry a cursor")
	}
	if f.Calls()[1].Query["cursor"] != "c1" || f.Calls()[1].Query["reason"] != "manual" {
		t.Fatalf("%v", f.Calls()[1].Query)
	}
}

func TestAnIteratorYieldsTheErrorOnceAndStops(t *testing.T) {
	c, _ := newFake(t, apiErr(403, "api_key_not_accepted", "no"))
	n := 0
	for _, err := range c.Contacts.All(ctx, nil) {
		n++
		if e := asError(t, err); e.Name != "api_key_not_accepted" || e.StatusCode != 403 {
			t.Fatalf("%+v", e)
		}
	}
	if n != 1 {
		t.Fatalf("yielded %d times", n)
	}
}

func TestSuppressionsRemoveSendsTheRecordedReasonAsTheDeleteBody(t *testing.T) {
	c, f := newFake(t, ok(map[string]any{"removed": true}))
	if err := c.Suppressions.Remove(ctx, "sup-1", "Customer asked us to resume mail"); err != nil {
		t.Fatal(err)
	}
	if f.Calls()[0].Method != "DELETE" || f.Calls()[0].Path != "/v1/suppressions/sup-1" || f.Calls()[0].json(t)["reason"] != "Customer asked us to resume mail" {
		t.Fatalf("%+v", f.Calls()[0])
	}
}

func TestSandboxesSendUsesTheSandboxToken(t *testing.T) {
	c, f := newFake(t, reply{Status: 201, Body: map[string]any{"id": "sbx-msg"}})
	res, err := c.Sandboxes.Send(ctx, "sc_sbx_abc", msg)
	if err != nil || res.ID != "sbx-msg" {
		t.Fatal(err, res)
	}
	if f.Calls()[0].Path != "/v1/sandbox/emails" || f.Calls()[0].Header.Get("Authorization") != "Bearer sc_sbx_abc" {
		t.Fatalf("%+v", f.Calls()[0])
	}
}

func TestContactsImportIsMultipartWithNoIdempotencyKeyAndNeverRetried(t *testing.T) {
	c, f := newFake(t, apiErr(503, "unavailable", "down"))
	_, err := c.Contacts.Import(ctx, &ImportContactsParams{File: []byte("name,email\nAda,ada@lumen.io\n"), Filename: "contacts.csv", OnDuplicate: "update"})
	if asError(t, err).StatusCode != 503 || len(f.Calls()) != 1 {
		t.Fatalf("err %v calls %d", err, len(f.Calls()))
	}
	call := f.Calls()[0]
	if call.Header.Get("Idempotency-Key") != "" || !strings.HasPrefix(call.Header.Get("Content-Type"), "multipart/form-data; boundary=") {
		t.Fatalf("headers %v", call.Header)
	}
	req, _ := http.NewRequest("POST", "/", strings.NewReader(string(call.Body)))
	req.Header.Set("Content-Type", call.Header.Get("Content-Type"))
	if err := req.ParseMultipartForm(1 << 20); err != nil {
		t.Fatal(err)
	}
	if req.FormValue("onDuplicate") != "update" {
		t.Fatal("onDuplicate missing")
	}
	fh := req.MultipartForm.File["file"][0]
	file, _ := fh.Open()
	data := make([]byte, fh.Size)
	_, _ = file.Read(data)
	if fh.Filename != "contacts.csv" || fh.Header.Get("Content-Type") != "text/csv" || string(data) != "name,email\nAda,ada@lumen.io\n" {
		t.Fatalf("file %s %s %q", fh.Filename, fh.Header.Get("Content-Type"), data)
	}
}

func TestAVcfImportIsSentAsTextVcard(t *testing.T) {
	c, f := newFake(t, ok(map[string]any{}))
	_, _ = c.Contacts.ImportPreview(ctx, &ImportContactsParams{File: []byte("BEGIN:VCARD\r\nEND:VCARD\r\n"), Filename: "people.vcf"})
	if f.Calls()[0].Path != "/v1/contacts/import/preview" || !strings.Contains(string(f.Calls()[0].Body), "Content-Type: text/vcard") {
		t.Fatalf("%s", f.Calls()[0].Body)
	}
}

func TestTemplateAssetsUploadBase64EncodesBytes(t *testing.T) {
	c, f := newFake(t, reply{Status: 201, Body: map[string]any{"id": "a", "url": "https://x/y.png"}})
	a, err := c.Templates.Assets.Upload(ctx, &UploadTemplateAssetParams{MimeType: "image/png", Data: []byte{1, 2, 3}, Name: "logo"})
	if err != nil || a.URL != "https://x/y.png" {
		t.Fatal(err, a)
	}
	if string(f.Calls()[0].Body) != `{"base64":"AQID","mimeType":"image/png","name":"logo"}` {
		t.Fatalf("%s", f.Calls()[0].Body)
	}
}

func TestTextEndpointsReturnTheBodyAsAString(t *testing.T) {
	c, _ := newFake(t, reply{Body: "<html>hï</html>", Headers: map[string]string{"Content-Type": "text/html"}})
	html, err := c.Templates.HTML(ctx, "tpl_1")
	if err != nil || html != "<html>hï</html>" {
		t.Fatal(err, html)
	}
}

func TestAnAttachmentDownloadReturnsBytesTypeAndFilename(t *testing.T) {
	c, f := newFake(t, reply{Body: "%PDF", Headers: map[string]string{
		"Content-Type": "application/pdf", "Content-Disposition": "attachment; filename*=UTF-8''Rechnung%20M%C3%A4rz.pdf"}})
	file, err := c.Mailboxes.Messages.Attachment(ctx, "m1", "msg1", "p1")
	if err != nil {
		t.Fatal(err)
	}
	if string(file.Content) != "%PDF" || file.ContentType != "application/pdf" || file.Filename != "Rechnung März.pdf" {
		t.Fatalf("%+v", file)
	}
	if f.Calls()[0].Query["download"] != "1" || f.Calls()[0].Header.Get("Accept") != "*/*" {
		t.Fatalf("%+v", f.Calls()[0])
	}
}

func TestPathSegmentsAreEncoded(t *testing.T) {
	c, f := newFake(t, ok(map[string]any{}))
	_, _ = c.Templates.Get(ctx, "a/b?c")
	if f.Calls()[0].Path != "/v1/templates/a%2Fb%3Fc" {
		t.Fatalf("%s", f.Calls()[0].Path)
	}
}

func TestNoKeyAnywhereIsAClearAPIKeyMissingAndNoRequestIsMade(t *testing.T) {
	t.Setenv("SENDCLIPPER_API_KEY", "")
	_, f := newFake(t)
	c := NewClient("", WithBaseURL(f.srv.URL))
	_, err := c.Domains.List(ctx)
	if e := asError(t, err); e.Name != "api_key_missing" || e.StatusCode != 401 || len(f.Calls()) != 0 {
		t.Fatalf("%+v", e)
	}
}

func TestTheKeyAndBaseURLFallBackToTheEnvironmentAndATrailingV1IsTolerated(t *testing.T) {
	_, f := newFake(t, ok([]any{}))
	t.Setenv("SENDCLIPPER_API_KEY", "sc_live_env")
	t.Setenv("SENDCLIPPER_BASE_URL", f.srv.URL+"/v1/")
	c := NewClient("")
	if _, err := c.Domains.List(ctx); err != nil {
		t.Fatal(err)
	}
	if c.BaseURL() != f.srv.URL || f.Calls()[0].Path != "/v1/domains" || f.Calls()[0].Header.Get("Authorization") != "Bearer sc_live_env" {
		t.Fatalf("%s %+v", c.BaseURL(), f.Calls()[0])
	}
	_ = os.Unsetenv("SENDCLIPPER_BASE_URL")
}

func TestDefaultAndPerCallHeadersAreSent(t *testing.T) {
	c, f := newFake(t, ok([]any{}))
	WithDefaultHeader("X-App", "shop")(c)
	_, _ = c.Domains.List(ctx, WithHeader("X-Trace-Id", "t1"))
	if f.Calls()[0].Header.Get("X-App") != "shop" || f.Calls()[0].Header.Get("X-Trace-Id") != "t1" {
		t.Fatalf("%v", f.Calls()[0].Header)
	}
}

func TestARedirectToAnotherHostDropsTheAPIKey(t *testing.T) {
	_, other := newFake(t, ok([]any{}))
	c, f := newFake(t, reply{Status: 302, Headers: map[string]string{"Location": strings.Replace(other.srv.URL, "127.0.0.1", "localhost", 1) + "/v1/domains"}})
	if _, err := c.Domains.List(ctx); err != nil {
		t.Fatal(err)
	}
	if f.Calls()[0].Header.Get("Authorization") == "" || other.Calls()[0].Header.Get("Authorization") != "" {
		t.Fatalf("first %q, redirected %q", f.Calls()[0].Header.Get("Authorization"), other.Calls()[0].Header.Get("Authorization"))
	}
}

func TestBatchSendPostsEmailsAndReturnsPerItemResults(t *testing.T) {
	r := map[string]any{
		"data": []any{
			map[string]any{"index": 0, "status": "accepted", "id": "e1", "messageId": "m1"},
			map[string]any{"index": 1, "status": "failed", "error": map[string]any{"code": "template_not_published", "message": "draft"}},
		},
		"summary": map[string]any{"submitted": 2, "accepted": 1, "failed": 1},
	}
	c, f := newFake(t, ok(r), ok(r))
	items := []SendEmailParams{*msg, {From: "a@acme.io", To: "y@lumen.io", TemplateID: "tpl_draft"}}
	res, err := c.Batch.Send(ctx, items)
	if err != nil || res.Summary.Accepted != 1 || res.Data[1].Error.Code != "template_not_published" {
		t.Fatal(err, res)
	}
	_, _ = c.Emails.SendBatch(ctx, items)
	for _, call := range f.Calls() {
		want := `{"emails":[{"from":"a@acme.io","to":"b@lumen.io","subject":"s","html":"h"},{"from":"a@acme.io","to":"y@lumen.io","templateId":"tpl_draft"}]}`
		if call.Path != "/v1/emails/batch" || string(call.Body) != want || call.Header.Get("Idempotency-Key") == "" {
			t.Fatalf("%s %s", call.Path, call.Body)
		}
	}
}

func TestSandboxWaitForPollsUntilAMatchThenReadsIt(t *testing.T) {
	summary := map[string]any{"id": "m2", "subject": "Hi", "trappedAt": "2026-10-16T10:00:00.000Z"}
	full := map[string]any{"id": "m2", "subject": "Hi", "trappedAt": "2026-10-16T10:00:00.000Z", "bodyHtml": "<p>Hi</p>"}
	c, f := newFake(t, ok([]any{}), ok([]any{summary}), ok(full))
	m, err := c.Sandboxes.Messages.WaitFor(ctx, "sbx1", &WaitForOptions{Match: func(m SandboxMessageSummary) bool { return m.Subject == "Hi" }, Interval: time.Millisecond})
	if err != nil || *m.BodyHTML != "<p>Hi</p>" {
		t.Fatal(err, m)
	}
	paths := []string{f.Calls()[0].Path, f.Calls()[1].Path, f.Calls()[2].Path}
	if strings.Join(paths, " ") != "/v1/sandboxes/sbx1/messages /v1/sandboxes/sbx1/messages /v1/sandboxes/sbx1/messages/m2" || f.Calls()[0].Query["limit"] != "50" {
		t.Fatalf("%v %v", paths, f.Calls()[0].Query)
	}
}

func TestSandboxWaitForEndsWithTimeoutWhenTheContextDoes(t *testing.T) {
	replies := make([]reply, 200)
	for i := range replies {
		replies[i] = ok([]any{})
	}
	c, _ := newFake(t, replies...)
	cctx, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	_, err := c.Sandboxes.Messages.WaitFor(cctx, "sbx1", &WaitForOptions{Interval: 5 * time.Millisecond})
	if e := asError(t, err); e.Name != "timeout" || e.StatusCode != 0 {
		t.Fatalf("%+v", e)
	}
}

// listRoutes answer a JSON array on GET.
var listRoutes = map[string]bool{
	"/v1/templates": true, "/v1/templates/projects": true, "/v1/templates/tags": true,
	"/v1/templates/assets": true, "/v1/sandboxes": true, "/v1/webhook-endpoints": true, "/v1/api-keys": true,
}

func TestEveryMethodReachesTheRouteTheAPIServes(t *testing.T) {
	cases := []struct {
		method, path string
		fn           func(*Client) error
	}{
		{"POST", "/v1/domains", func(c *Client) error {
			_, e := c.Domains.Create(ctx, &CreateDomainParams{Name: "send.acme.io", Region: EUWest1})
			return e
		}},
		{"GET", "/v1/domains/d1/records", func(c *Client) error { _, e := c.Domains.Records(ctx, "d1"); return e }},
		{"POST", "/v1/domains/d1/verify", func(c *Client) error { _, e := c.Domains.Verify(ctx, "d1"); return e }},
		{"POST", "/v1/domains/d1/receiving", func(c *Client) error { _, e := c.Domains.EnableReceiving(ctx, "d1"); return e }},
		{"DELETE", "/v1/domains/d1/receiving", func(c *Client) error { _, e := c.Domains.DisableReceiving(ctx, "d1"); return e }},
		{"PATCH", "/v1/domains/d1/region", func(c *Client) error { _, e := c.Domains.SetRegion(ctx, "d1", USEast1); return e }},
		{"GET", "/v1/domains/d1/deletion", func(c *Client) error { _, e := c.Domains.DeletionPreview(ctx, "d1"); return e }},
		{"DELETE", "/v1/domains/d1", func(c *Client) error { return c.Domains.Remove(ctx, "d1") }},
		{"GET", "/v1/emails/stats/summary", func(c *Client) error { _, e := c.Emails.Stats(ctx); return e }},
		{"GET", "/v1/templates", func(c *Client) error { _, e := c.Templates.List(ctx, &ListTemplatesParams{Tag: "t1"}); return e }},
		{"PATCH", "/v1/templates/t1", func(c *Client) error {
			n := "n"
			_, e := c.Templates.Update(ctx, "t1", &UpdateTemplateParams{Name: &n})
			return e
		}},
		{"DELETE", "/v1/templates/t1", func(c *Client) error { return c.Templates.Remove(ctx, "t1") }},
		{"POST", "/v1/templates/t1/duplicate", func(c *Client) error { _, e := c.Templates.Duplicate(ctx, "t1"); return e }},
		{"POST", "/v1/templates/t1/publish", func(c *Client) error { _, e := c.Templates.Publish(ctx, "t1"); return e }},
		{"POST", "/v1/templates/t1/unpublish", func(c *Client) error { _, e := c.Templates.Unpublish(ctx, "t1"); return e }},
		{"PUT", "/v1/templates/t1/tags", func(c *Client) error { _, e := c.Templates.SetTags(ctx, "t1", []string{"g1"}); return e }},
		{"GET", "/v1/templates/projects", func(c *Client) error { _, e := c.Templates.Projects(ctx); return e }},
		{"GET", "/v1/templates/tags", func(c *Client) error { _, e := c.Templates.Tags.List(ctx); return e }},
		{"POST", "/v1/templates/tags", func(c *Client) error { _, e := c.Templates.Tags.Create(ctx, "n", "blue"); return e }},
		{"PATCH", "/v1/templates/tags/g1", func(c *Client) error { _, e := c.Templates.Tags.Update(ctx, "g1", "", "red"); return e }},
		{"DELETE", "/v1/templates/tags/g1", func(c *Client) error { _, e := c.Templates.Tags.Remove(ctx, "g1"); return e }},
		{"GET", "/v1/templates/assets", func(c *Client) error { _, e := c.Templates.Assets.List(ctx); return e }},
		{"GET", "/v1/sandboxes", func(c *Client) error { _, e := c.Sandboxes.List(ctx); return e }},
		{"GET", "/v1/sandboxes/s1", func(c *Client) error { _, e := c.Sandboxes.Get(ctx, "s1"); return e }},
		{"PATCH", "/v1/sandboxes/s1", func(c *Client) error { _, e := c.Sandboxes.Update(ctx, "s1", &SandboxParams{Name: "n"}); return e }},
		{"DELETE", "/v1/sandboxes/s1", func(c *Client) error { _, e := c.Sandboxes.Remove(ctx, "s1"); return e }},
		{"GET", "/v1/sandboxes/s1/credentials", func(c *Client) error { _, e := c.Sandboxes.Credentials(ctx, "s1"); return e }},
		{"POST", "/v1/sandboxes/s1/credentials", func(c *Client) error { _, e := c.Sandboxes.ResetCredentials(ctx, "s1"); return e }},
		{"GET", "/v1/sandboxes/s1/messages/m1/raw", func(c *Client) error { _, e := c.Sandboxes.Messages.Raw(ctx, "s1", "m1"); return e }},
		{"DELETE", "/v1/sandboxes/s1/messages/m1", func(c *Client) error { return c.Sandboxes.Messages.Remove(ctx, "s1", "m1") }},
		{"DELETE", "/v1/sandboxes/s1/messages", func(c *Client) error { _, e := c.Sandboxes.Messages.Clear(ctx, "s1"); return e }},
		{"POST", "/v1/sandboxes/s1/read-all", func(c *Client) error { _, e := c.Sandboxes.Messages.MarkAllRead(ctx, "s1"); return e }},
		{"GET", "/v1/webhook-endpoints", func(c *Client) error { _, e := c.Webhooks.List(ctx); return e }},
		{"POST", "/v1/webhook-endpoints", func(c *Client) error {
			_, e := c.Webhooks.Create(ctx, &CreateWebhookParams{URL: "https://x.io", Events: []WebhookEventType{EventDelivered}})
			return e
		}},
		{"PATCH", "/v1/webhook-endpoints/w1", func(c *Client) error {
			_, e := c.Webhooks.Update(ctx, "w1", &UpdateWebhookParams{Active: Bool(false)})
			return e
		}},
		{"DELETE", "/v1/webhook-endpoints/w1", func(c *Client) error { return c.Webhooks.Remove(ctx, "w1") }},
		{"POST", "/v1/webhook-endpoints/w1/rotate-secret", func(c *Client) error { _, e := c.Webhooks.RotateSecret(ctx, "w1"); return e }},
		{"GET", "/v1/webhook-dispatches", func(c *Client) error { _, e := c.Webhooks.Dispatches.List(ctx, nil); return e }},
		{"GET", "/v1/webhook-dispatches/x1", func(c *Client) error { _, e := c.Webhooks.Dispatches.Get(ctx, "x1"); return e }},
		{"POST", "/v1/webhook-dispatches/x1/replay", func(c *Client) error { _, e := c.Webhooks.Dispatches.Replay(ctx, "x1"); return e }},
		{"GET", "/v1/contacts", func(c *Client) error { _, e := c.Contacts.List(ctx, nil); return e }},
		{"POST", "/v1/contacts", func(c *Client) error {
			_, e := c.Contacts.Create(ctx, &CreateContactParams{Name: "Ada", Email: "a@x.io"})
			return e
		}},
		{"GET", "/v1/contacts/export", func(c *Client) error { _, e := c.Contacts.Export(ctx, &ExportContactsParams{Format: "vcf"}); return e }},
		{"GET", "/v1/audiences", func(c *Client) error { _, e := c.Audiences.List(ctx); return e }},
		{"POST", "/v1/suppressions", func(c *Client) error {
			_, e := c.Suppressions.Create(ctx, &CreateSuppressionParams{Email: "a@x.io"})
			return e
		}},
		{"GET", "/v1/suppressions/export", func(c *Client) error {
			_, e := c.Suppressions.Export(ctx, &ListSuppressionsParams{Reason: "manual", Cursor: "c", Limit: 5})
			return e
		}},
		{"GET", "/v1/mailboxes/mb1", func(c *Client) error { _, e := c.Mailboxes.Get(ctx, "mb1"); return e }},
		{"POST", "/v1/mailboxes", func(c *Client) error {
			_, e := c.Mailboxes.Create(ctx, &CreateMailboxParams{DomainID: "d1", LocalPart: "support"})
			return e
		}},
		{"PATCH", "/v1/mailboxes/mb1", func(c *Client) error {
			_, e := c.Mailboxes.Update(ctx, "mb1", &UpdateMailboxParams{Archived: Bool(true)})
			return e
		}},
		{"DELETE", "/v1/mailboxes/mb1", func(c *Client) error { _, e := c.Mailboxes.Archive(ctx, "mb1"); return e }},
		{"GET", "/v1/mailboxes/mb1/messages", func(c *Client) error {
			_, e := c.Mailboxes.Messages.List(ctx, "mb1", &ListMailboxMessagesParams{Folder: "inbox"})
			return e
		}},
		{"GET", "/v1/mailboxes/mb1/messages/m1", func(c *Client) error { _, e := c.Mailboxes.Messages.Get(ctx, "mb1", "m1"); return e }},
		{"POST", "/v1/automations/a1/enrollments", func(c *Client) error { _, e := c.Automations.Enroll(ctx, "a1", "ada@x.io"); return e }},
		{"GET", "/v1/workspace", func(c *Client) error { _, e := c.Workspace.Get(ctx); return e }},
		{"GET", "/v1/api-keys", func(c *Client) error { _, e := c.APIKeys.List(ctx); return e }},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			body := any(map[string]any{})
			if listRoutes[tc.path] && tc.method == "GET" {
				body = []any{}
			}
			c, f := newFake(t, ok(body))
			if err := tc.fn(c); err != nil {
				t.Fatal(err)
			}
			if f.Calls()[0].Method != tc.method || f.Calls()[0].Path != tc.path {
				t.Fatalf("got %s %s", f.Calls()[0].Method, f.Calls()[0].Path)
			}
			if tc.method != "GET" && f.Calls()[0].Header.Get("Idempotency-Key") == "" {
				t.Fatal("no idempotency key on a write")
			}
		})
	}
}

func TestSuppressionsExportDropsPagingParams(t *testing.T) {
	c, f := newFake(t, reply{Body: "email\n", Headers: map[string]string{"Content-Type": "text/csv"}})
	csv, err := c.Suppressions.Export(ctx, &ListSuppressionsParams{Reason: "manual", Cursor: "c", Limit: 5})
	if err != nil || csv != "email\n" || len(f.Calls()[0].Query) != 1 || f.Calls()[0].Query["reason"] != "manual" {
		t.Fatalf("%v %q %v", err, csv, f.Calls()[0].Query)
	}
}

func TestAnUndecodableAnswerIsAnInvalidResponseError(t *testing.T) {
	c, _ := newFake(t, ok(map[string]any{"createdAt": "not a time"}))
	_, err := c.Workspace.Get(ctx)
	if e := asError(t, err); e.Name != "invalid_response" {
		t.Fatalf("%+v", e)
	}
}

func TestVersionMatchesTheChangelog(t *testing.T) {
	b, err := os.ReadFile("CHANGELOG.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "## "+Version+" ") {
		t.Fatalf("CHANGELOG.md has no entry for %s", Version)
	}
}
