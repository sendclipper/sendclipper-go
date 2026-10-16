package sendclipper

// Live end-to-end: this package against a real SendClipper API, with an
// sc_live_ key minted for the run.
//
//	LIVE_API_URL=http://localhost:4000 go test -run Live -v ./...
//
// It refuses anything that is not localhost: it creates and deletes data and
// sends mail through whatever transport the API has, which must be the local
// SES emulator (checked on /v1/health first). Skipped when LIVE_API_URL is unset.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

type live struct {
	api, jwt, keyID, run, from string
	c                          *Client
}

func liveSetup(t *testing.T) *live {
	t.Helper()
	api := os.Getenv("LIVE_API_URL")
	if api == "" {
		t.Skip("LIVE_API_URL is not set")
	}
	u, _ := url.Parse(api)
	if h := u.Hostname(); h != "localhost" && h != "127.0.0.1" {
		t.Fatalf("the live test runs against a local API only, not %s", h)
	}
	l := &live{api: api, run: fmt.Sprintf("sdkgo%x", time.Now().UnixMilli())}
	var health struct {
		Backends map[string]string `json:"backends"`
	}
	l.raw(t, "GET", "/health", nil, &health)
	if health.Backends["sesSend"] != "local" || health.Backends["objectStore"] != "filesystem" {
		t.Fatalf("refusing a non-local API: %v", health.Backends)
	}
	email, password := os.Getenv("LIVE_EMAIL"), os.Getenv("LIVE_PASSWORD")
	if email == "" {
		email, password = "enis@sendclipper.dev", "password"
	}
	var login struct{ Token string }
	l.raw(t, "POST", "/auth/login", map[string]string{"email": email, "password": password}, &login)
	l.jwt = login.Token
	var key struct{ ID, Token string }
	l.raw(t, "POST", "/api-keys", map[string]string{"name": l.run + "-full", "permission": "full"}, &key)
	l.keyID = key.ID
	t.Cleanup(func() { l.raw(t, "DELETE", "/api-keys/"+l.keyID, nil, nil) })
	l.c = NewClient(key.Token, WithBaseURL(api))

	domains, err := l.c.Domains.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range domains {
		if d.Status == "verified" && !strings.HasSuffix(d.Name, ".example.com") {
			l.from = "sdk-live@" + d.Name
			break
		}
	}
	if l.from == "" {
		t.Fatal("the seeded workspace needs a verified domain")
	}
	return l
}

func (l *live) raw(t *testing.T, method, path string, body, out any) {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, l.api+"/v1"+path, r)
	req.Header.Set("Content-Type", "application/json")
	if l.jwt != "" {
		req.Header.Set("Authorization", "Bearer "+l.jwt)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 300 {
		t.Fatalf("%s %s -> %d %s", method, path, res.StatusCode, data)
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			t.Fatal(err)
		}
	}
}

func must[T any](t *testing.T, what string) func(T, error) T {
	return func(v T, err error) T {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		return v
	}
}

func TestLive(t *testing.T) {
	l := liveSetup(t)
	ctx := context.Background()
	c := l.c

	t.Run("every list and read decodes into its typed struct", func(t *testing.T) {
		ws := must[*Workspace](t, "workspace")(c.Workspace.Get(ctx))
		keys := must[[]APIKey](t, "api keys")(c.APIKeys.List(ctx))
		found := false
		for _, k := range keys {
			found = found || k.ID == l.keyID
		}
		if ws.PublicID == "" || !found {
			t.Fatalf("workspace %q, key found %v", ws.PublicID, found)
		}
		domains := must[[]Domain](t, "domains")(c.Domains.List(ctx))
		must[*DomainRecords](t, "records")(c.Domains.Records(ctx, domains[0].ID))
		must[*EmailStats](t, "stats")(c.Emails.Stats(ctx))
		must[*EmailList](t, "emails")(c.Emails.List(ctx, &ListEmailsParams{PageSize: 5}))
		tpls := must[[]TemplateSummary](t, "templates")(c.Templates.List(ctx, nil))
		if len(tpls) > 0 {
			must[*Template](t, "template")(c.Templates.Get(ctx, tpls[0].ID))
		}
		must[[]TemplateTag](t, "tags")(c.Templates.Tags.List(ctx))
		must[[]TemplateAsset](t, "assets")(c.Templates.Assets.List(ctx))
		must[[]NamedRef](t, "projects")(c.Templates.Projects(ctx))
		boxes := must[[]Sandbox](t, "sandboxes")(c.Sandboxes.List(ctx))
		if len(boxes) > 0 {
			must[*SandboxCredentials](t, "credentials")(c.Sandboxes.Credentials(ctx, boxes[0].ID))
			msgs := must[[]SandboxMessageSummary](t, "sandbox messages")(c.Sandboxes.Messages.List(ctx, boxes[0].ID, &ListSandboxMessagesParams{Limit: 5}))
			if len(msgs) > 0 {
				must[*SandboxMessage](t, "sandbox message")(c.Sandboxes.Messages.Get(ctx, boxes[0].ID, msgs[0].ID))
			}
		}
		must[[]WebhookEndpoint](t, "webhooks")(c.Webhooks.List(ctx))
		must[*WebhookDispatchPage](t, "dispatches")(c.Webhooks.Dispatches.List(ctx, &ListWebhookDispatchesParams{State: "all", Limit: 5}))
		must[*ContactList](t, "contacts")(c.Contacts.List(ctx, &ListContactsParams{PageSize: 5}))
		must[*AudienceList](t, "audiences")(c.Audiences.List(ctx))
		must[*SuppressionList](t, "suppressions")(c.Suppressions.List(ctx, &ListSuppressionsParams{Limit: 5}))
		boxesM := must[[]Mailbox](t, "mailboxes")(c.Mailboxes.List(ctx, false))
		for _, mb := range boxesM {
			page := must[*MailboxMessageList](t, "mailbox messages")(c.Mailboxes.Messages.List(ctx, mb.ID, &ListMailboxMessagesParams{Limit: 5}))
			if len(page.Items) > 0 {
				must[*MailboxMessage](t, "mailbox message")(c.Mailboxes.Messages.Get(ctx, mb.ID, page.Items[0].ID))
				break
			}
		}
	})

	t.Run("send then get, and a repeated idempotency key answers the first send", func(t *testing.T) {
		p := &SendEmailParams{From: l.from, To: "ada+" + l.run + "@lumen.io", Subject: "SDK live " + l.run + " Grüße <ok>", HTML: "<p>Hello from Go</p>", Text: "Hello from Go"}
		first := must[*SentEmail](t, "send")(c.Emails.Send(ctx, p, WithIdempotencyKey(l.run+"-welcome")))
		var h http.Header
		again := must[*SentEmail](t, "send replay")(c.Emails.Send(ctx, p, WithIdempotencyKey(l.run+"-welcome"), WithResponseHeaders(&h)))
		if again.ID != first.ID || h.Get("Idempotency-Replayed") != "true" {
			t.Fatalf("replay %s vs %s, header %q", again.ID, first.ID, h.Get("Idempotency-Replayed"))
		}
		d := must[*EmailDetail](t, "get")(c.Emails.Get(ctx, first.ID))
		if d.Subject != p.Subject || d.Events == nil {
			t.Fatalf("%+v", d)
		}
	})

	t.Run("an API refusal is an *Error with the API's code", func(t *testing.T) {
		_, err := c.Emails.Send(ctx, &SendEmailParams{From: "nobody@not-a-domain-we-own.test", To: "a@lumen.io", Subject: "s", HTML: "h"})
		if e := asError(t, err); e.StatusCode != 403 || e.Name == "" {
			t.Fatalf("%+v", e)
		}
		_, err = c.Emails.Send(ctx, &SendEmailParams{From: l.from, To: "not-an-address", Subject: "s", HTML: "h"})
		if e := asError(t, err); e.Name != "validation_failed" {
			t.Fatalf("%+v", e)
		}
	})

	t.Run("batch accepts and refuses items independently", func(t *testing.T) {
		res := must[*SendBatchResponse](t, "batch")(c.Batch.Send(ctx, []SendEmailParams{
			{From: l.from, To: "b1+" + l.run + "@lumen.io", Subject: "batch 1", HTML: "<p>1</p>"},
			{From: l.from, To: "b2+" + l.run + "@lumen.io", TemplateID: "tpl_doesnotexist0"},
		}))
		if res.Summary.Submitted != 2 || res.Data[0].Status != "accepted" || res.Data[1].Status != "failed" {
			t.Fatalf("%+v", res)
		}
	})

	t.Run("templates: create, preview, publish, send by id, remove", func(t *testing.T) {
		tpl := must[*Template](t, "create")(c.Templates.Create(ctx, &CreateTemplateParams{
			Name: l.run + " welcome", Subject: "Welcome {{first_name|there}}",
			Blocks: []TemplateBlock{{"type": "text", "text": "Hi {{first_name|there}}, welcome aboard."}},
		}))
		defer func() { _ = c.Templates.Remove(ctx, tpl.ID) }()
		if p := must[*TemplatePreview](t, "preview")(c.Templates.Preview(ctx, tpl.ID, map[string]any{"first_name": "Ada"})); p.Subject != "Welcome Ada" {
			t.Fatal(p.Subject)
		}
		if p := must[*TemplatePreview](t, "preview nil")(c.Templates.Preview(ctx, tpl.ID, nil)); p.Subject != "Welcome there" {
			t.Fatal(p.Subject)
		}
		must[*Template](t, "publish")(c.Templates.Publish(ctx, tpl.ID))
		sent := must[*SentEmail](t, "send template")(c.Emails.Send(ctx, &SendEmailParams{From: l.from, To: "t+" + l.run + "@lumen.io", TemplateID: tpl.PublicID, Variables: map[string]any{"first_name": "Ada"}}))
		if sent.Subject != "Welcome Ada" {
			t.Fatal(sent.Subject)
		}
		if html := must[string](t, "html")(c.Templates.HTML(ctx, tpl.ID)); !strings.Contains(html, "Hi") {
			t.Fatal(html)
		}
	})

	t.Run("sandboxes: reset credentials, send with the token, wait, raw", func(t *testing.T) {
		box := must[*Sandbox](t, "create")(c.Sandboxes.Create(ctx, &SandboxParams{Name: l.run + "-box"}))
		defer func() { _, _ = c.Sandboxes.Remove(ctx, box.ID) }()
		creds := must[*ResetSandboxCredentials](t, "reset")(c.Sandboxes.ResetCredentials(ctx, box.ID))
		if !strings.HasPrefix(creds.APIToken, "sc_sbx_") {
			t.Fatal(creds.APIToken)
		}
		subject := "trapped " + l.run
		must[*SandboxSendResult](t, "sandbox send")(c.Sandboxes.Send(ctx, creds.APIToken, &SendEmailParams{From: "app@example.com", To: "someone@example.com", Subject: subject, HTML: "<p>trap</p>"}))
		wctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		m := must[*SandboxMessage](t, "wait")(c.Sandboxes.Messages.WaitFor(wctx, box.ID, &WaitForOptions{Match: func(m SandboxMessageSummary) bool { return m.Subject == subject }, Interval: 300 * time.Millisecond}))
		if raw := must[string](t, "raw")(c.Sandboxes.Messages.Raw(ctx, box.ID, m.ID)); !strings.Contains(raw, "Subject: "+subject) {
			t.Fatal(raw)
		}
	})

	t.Run("webhooks: the secret comes once and Verify accepts it", func(t *testing.T) {
		hook := must[*WebhookEndpointWithSecret](t, "create")(c.Webhooks.Create(ctx, &CreateWebhookParams{URL: "http://127.0.0.1:9/sdk-live-go", Events: []WebhookEventType{EventDelivered, EventBounced}, AllowInsecureHTTP: true}))
		defer func() { _ = c.Webhooks.Remove(ctx, hook.ID) }()
		body := []byte(`{"id":"evt_live","event":"email.delivered","createdAt":"2026-10-16T10:00:00Z","data":{}}`)
		h := http.Header{}
		h.Set(SignatureHeader, WebhookSignatureHeader(hook.Secret, body, 0))
		if ev, err := c.Webhooks.Verify(body, h, hook.Secret); err != nil || ev.ID != "evt_live" {
			t.Fatal(err)
		}
		up := must[*WebhookEndpoint](t, "update")(c.Webhooks.Update(ctx, hook.ID, &UpdateWebhookParams{Events: []WebhookEventType{EventOpened}}))
		if len(up.Events) != 1 || up.Events[0] != EventOpened {
			t.Fatal(up.Events)
		}
		if rot := must[*WebhookEndpointWithSecret](t, "rotate")(c.Webhooks.RotateSecret(ctx, hook.ID)); rot.Secret == hook.Secret {
			t.Fatal("secret did not change")
		}
	})

	t.Run("suppressions: add, find, remove with a reason", func(t *testing.T) {
		email := "blocked+" + l.run + "@lumen.io"
		created := must[*SuppressionCreated](t, "create")(c.Suppressions.Create(ctx, &CreateSuppressionParams{Email: email, Note: "sdk live test"}))
		if !created.Created {
			t.Fatal("not created")
		}
		found := false
		for s, err := range c.Suppressions.All(ctx, &ListSuppressionsParams{Q: email}) {
			if err != nil {
				t.Fatal(err)
			}
			found = found || s.Email == email
		}
		if !found {
			t.Fatal("not found")
		}
		if err := c.Suppressions.Remove(ctx, created.Row.ID, "Removed by the Go SDK live test run"); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("contacts, an import preview and iterators", func(t *testing.T) {
		ct := must[*Contact](t, "create")(c.Contacts.Create(ctx, &CreateContactParams{Name: "SDK Live", Email: "contact+" + l.run + "@lumen.io"}))
		if ct.Email != "contact+"+l.run+"@lumen.io" {
			t.Fatal(ct.Email)
		}
		if p := must[map[string]any](t, "import preview")(c.Contacts.ImportPreview(ctx, &ImportContactsParams{File: []byte("name,email\nGo Live,go+" + l.run + "@lumen.io\n")})); len(p) == 0 {
			t.Fatal("empty preview")
		}
		n := 0
		for e, err := range c.Emails.All(ctx, &ListEmailsParams{Search: l.run, PageSize: 2}) {
			if err != nil {
				t.Fatal(err)
			}
			if e.ID == "" {
				t.Fatal("empty id")
			}
			n++
		}
		if n < 3 {
			t.Fatalf("expected this run's sends in the log, saw %d", n)
		}
	})
}
