# SendClipper Go SDK

The official Go library for the [SendClipper](https://sendclipper.com) email API.

```bash
go get github.com/sendclipper/sendclipper-go
```

Go 1.23 or newer. No dependencies outside the standard library. API reference on [pkg.go.dev](https://pkg.go.dev/github.com/sendclipper/sendclipper-go).

## Setup

Create an API key in the dashboard under **API keys**.

```go
import "github.com/sendclipper/sendclipper-go"

client := sendclipper.NewClient("sc_live_xxxxxxxxxxxxxxxx")
// or: sendclipper.NewClient("")  reads SENDCLIPPER_API_KEY
```

A client is safe for concurrent use: make one and share it.

## Send an email

```go
email, err := client.Emails.Send(ctx, &sendclipper.SendEmailParams{
	From:    "hello@send.acme.io",
	To:      "ada@lumen.io",
	Subject: "Your receipt",
	HTML:    "<p>Thanks for your order.</p>",
	Text:    "Thanks for your order.",
})
if err != nil {
	var apiErr *sendclipper.Error
	if errors.As(err, &apiErr) {
		log.Println(apiErr.Name, apiErr.Message) // e.g. domain_not_verified
	}
	return err
}
fmt.Println("Email", email.ID, "accepted")
```

`From` must be a bare address on a domain you verified in SendClipper. Each message has one recipient in `To`; for several, use `Batch.Send`.

## Send with a template

Design and publish a template in the dashboard, then send it by id or by its send name:

```go
client.Emails.Send(ctx, &sendclipper.SendEmailParams{
	From:       "hello@send.acme.io",
	To:         "ada@lumen.io",
	TemplateID: "tpl_9f2b7c10ad42", // or Template: "welcome"
	Variables:  map[string]any{"first_name": "Ada"},
})
```

A tag with no value and no `{{tag|fallback}}` is refused by name (`template_variables_missing`), never sent blank.

## Send a batch

Up to 500 messages in one request. Each item is accepted or refused on its own:

```go
res, err := client.Batch.Send(ctx, []sendclipper.SendEmailParams{
	{From: "hello@send.acme.io", To: "ada@lumen.io", Subject: "Hi Ada", HTML: "<p>Hi</p>"},
	{From: "hello@send.acme.io", To: "alan@lumen.io", TemplateID: "tpl_9f2b7c10ad42", Variables: map[string]any{"first_name": "Alan"}},
})
for _, item := range res.Data {
	if item.Status == "failed" {
		log.Println(item.Index, item.Error.Code)
	}
}
```

## Errors

Every method returns the decoded response or an error. A refusal from the API, or a request that never got an answer, is a `*sendclipper.Error`:

```go
type Error struct {
	Name       string          // SendClipper's code: "validation_failed", "rate_limited", "network_error", ...
	Message    string          // a sentence for a person
	StatusCode int             // 0 when no answer arrived (network, timeout, cancelled context)
	Detail     json.RawMessage // code-specific facts
	Headers    http.Header     // the response headers, nil when no answer arrived
}
```

A `network_error` wraps the transport error, so `errors.Is(err, context.DeadlineExceeded)` works. For a successful call's headers, pass `sendclipper.WithResponseHeaders(&h)`.

## Retries and idempotency

Network errors, `429` and `5xx` answers are retried twice with backoff, honouring `Retry-After`. Every JSON write carries an `Idempotency-Key`, and the API answers a repeated key with the first result for 24 hours. A retried send therefore never goes out twice.

To make your own retries safe as well (a job that runs again after a crash), pass a key that names the logical operation:

```go
client.Emails.Send(ctx, params, sendclipper.WithIdempotencyKey("order-"+order.ID+"-receipt"))
```

## Options

The context bounds the whole call, retries included. Per call:

| Option | Meaning |
|---|---|
| `WithIdempotencyKey(key)` | Your own key for a write |
| `WithRequestTimeout(d)` | Per-attempt timeout (default 30 s) |
| `WithRequestMaxRetries(n)` | Retries for this call (default 2) |
| `WithHeader(name, value)` | An extra request header |
| `WithResponseHeaders(&h)` | Store the response headers |

Client options: `NewClient(key, WithBaseURL(url), WithHTTPClient(hc), WithTimeout(d), WithMaxRetries(n), WithDefaultHeader(name, value))`. The base URL also reads `SENDCLIPPER_BASE_URL`.

## Iterate

Lists that span pages have an `All` method that returns an iterator:

```go
for email, err := range client.Emails.All(ctx, &sendclipper.ListEmailsParams{Status: sendclipper.StatusBounced}) {
	if err != nil {
		return err // a failed page is yielded once, then the loop ends
	}
	fmt.Println(email.To)
}
```

## Verify webhooks

SendClipper signs each delivery. Verify it against the **raw** body, before decoding:

```go
http.HandleFunc("/hooks/sendclipper", func(w http.ResponseWriter, r *http.Request) {
	payload, _ := io.ReadAll(r.Body) // the raw body, before any json.Decode
	event, err := sendclipper.VerifyWebhook(payload, r.Header, os.Getenv("SENDCLIPPER_WEBHOOK_SECRET"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// event.ID is stable across retries: deduplicate on it
	log.Println(event.Event, event.Data.To)
})
```

A refusal is a `*sendclipper.WebhookVerificationError` whose `Reason` says which check failed. During a secret rotation a delivery carries two signatures and either one is accepted. A delivery signed more than five minutes away from now is refused. `event.Data.Raw` holds every field of the event's data. If you only hold the header's value, use `VerifyWebhookSignature`.

To test your handler locally, `sendclipper.WebhookSignatureHeader(secret, rawBody, 0)` builds a valid header.

## Sandboxes

A sandbox traps mail while you develop: nothing is delivered.

```go
box, _ := client.Sandboxes.Create(ctx, &sendclipper.SandboxParams{Name: "my-app-dev"})
creds, _ := client.Sandboxes.Credentials(ctx, box.ID) // SMTP host, port, username, password

// Or send through the sandbox's JSON door with its own token, using the same fields as Emails.Send:
token, _ := client.Sandboxes.ResetCredentials(ctx, box.ID)
client.Sandboxes.Send(ctx, token.APIToken, &sendclipper.SendEmailParams{From: "app@example.com", To: "test@example.com", Subject: "Hi", HTML: "<p>Hi</p>"})

// In a test: wait for the message to arrive (bound it with the context)
wctx, cancel := context.WithTimeout(ctx, 30*time.Second)
defer cancel()
msg, err := client.Sandboxes.Messages.WaitFor(wctx, box.ID, &sendclipper.WaitForOptions{
	Match: func(m sendclipper.SandboxMessageSummary) bool { return m.Subject == "Hi" },
})
```

## Everything else

| Service | Methods |
|---|---|
| `Emails` | `Send`, `Get`, `List`, `All`, `Stats` |
| `Batch` | `Send` |
| `Domains` | `List`, `Create`, `Records`, `Verify`, `EnableReceiving`, `DisableReceiving`, `SetRegion`, `DeletionPreview`, `Remove` |
| `Templates` | `List`, `Get`, `HTML`, `Create`, `Update`, `Remove`, `Duplicate`, `Publish`, `Unpublish`, `Preview`, `SetTags`, `Projects` |
| `Templates.Tags` | `List`, `Create`, `Update`, `Remove` |
| `Templates.Assets` | `List`, `Upload` |
| `Sandboxes` | `List`, `Get`, `Create`, `Update`, `Remove`, `Credentials`, `ResetCredentials`, `Send` |
| `Sandboxes.Messages` | `List`, `Get`, `Raw`, `Remove`, `Clear`, `MarkAllRead`, `WaitFor` |
| `Webhooks` | `List`, `Create`, `Update`, `Remove`, `RotateSecret`, `Deliveries`, `Verify` |
| `Webhooks.Dispatches` | `List`, `All`, `Get`, `Replay`, `ReplayAll` |
| `Contacts` | `List`, `All`, `Create`, `Import`, `ImportPreview`, `Export` |
| `Audiences` | `List` |
| `Suppressions` | `List`, `All`, `Create`, `Remove`, `Export` |
| `Mailboxes` | `List`, `Get`, `Create`, `Update`, `Archive` |
| `Mailboxes.Messages` | `List`, `All`, `Get`, `Attachment` |
| `Automations` | `Enroll` |
| `Workspace` | `Get` |
| `APIKeys` | `List` |

Management routes need a key with **Full access**. A sending-only key can call `Emails.Send` and `Batch.Send`.

Full API reference: <https://sendclipper.com/docs>.

## License

MIT
