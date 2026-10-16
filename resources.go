package sendclipper

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"iter"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"
)

// ---------------------------------------------------------------- emails

// EmailsService sends mail and reads the sent-mail log.
type EmailsService struct{ c *Client }

// Send sends one message. It returns once SendClipper accepted it (status
// queued); delivery is reported later through Get and webhooks.
//
// Every call carries an Idempotency-Key, so the SDK's own retries never send
// twice. Pass [WithIdempotencyKey] to make YOUR retries safe too.
func (s *EmailsService) Send(ctx context.Context, params *SendEmailParams, opts ...RequestOption) (*SentEmail, error) {
	out := new(SentEmail)
	return out, s.c.do(ctx, request{method: http.MethodPost, path: "/emails", body: params, out: out}, opts)
}

// SendBatch is the same as Batch.Send.
func (s *EmailsService) SendBatch(ctx context.Context, emails []SendEmailParams, opts ...RequestOption) (*SendBatchResponse, error) {
	return sendBatch(ctx, s.c, emails, opts)
}

// Get returns one message with its body and event timeline.
func (s *EmailsService) Get(ctx context.Context, id string, opts ...RequestOption) (*EmailDetail, error) {
	out := new(EmailDetail)
	return out, s.c.do(ctx, request{method: http.MethodGet, path: "/emails/" + seg(id), out: out}, opts)
}

// List returns one page of the sent-mail log, newest first.
func (s *EmailsService) List(ctx context.Context, params *ListEmailsParams, opts ...RequestOption) (*EmailList, error) {
	p := deref(params)
	out := new(EmailList)
	qs := q().str("search", p.Search).str("status", string(p.Status)).str("domain", p.Domain).
		str("broadcastId", p.BroadcastID).str("from", p.From).str("to", p.To).num("page", p.Page).num("pageSize", p.PageSize)
	return out, s.c.do(ctx, request{method: http.MethodGet, path: "/emails", query: qs.values(), out: out}, opts)
}

// All walks every message matching params, page after page:
//
//	for email, err := range client.Emails.All(ctx, nil) { ... }
//
// A failed page yields its error once and stops.
func (s *EmailsService) All(ctx context.Context, params *ListEmailsParams, opts ...RequestOption) iter.Seq2[EmailRow, error] {
	p := deref(params)
	if p.PageSize == 0 {
		p.PageSize = 100
	}
	return func(yield func(EmailRow, error) bool) {
		for page := 1; ; page++ {
			p.Page = page
			res, err := s.List(ctx, &p, opts...)
			if err != nil {
				yield(EmailRow{}, err)
				return
			}
			for _, row := range res.Items {
				if !yield(row, nil) {
					return
				}
			}
			if len(res.Items) < p.PageSize || page*p.PageSize >= res.Total {
				return
			}
		}
	}
}

// Stats counts messages per status.
func (s *EmailsService) Stats(ctx context.Context, opts ...RequestOption) (*EmailStats, error) {
	out := new(EmailStats)
	return out, s.c.do(ctx, request{method: http.MethodGet, path: "/emails/stats/summary", out: out}, opts)
}

// BatchService sends up to 500 messages in one request.
type BatchService struct{ c *Client }

// Send sends up to 500 messages (one recipient each). Every item is accepted
// or refused on its own; a refused item costs its siblings nothing.
func (s *BatchService) Send(ctx context.Context, emails []SendEmailParams, opts ...RequestOption) (*SendBatchResponse, error) {
	return sendBatch(ctx, s.c, emails, opts)
}

func sendBatch(ctx context.Context, c *Client, emails []SendEmailParams, opts []RequestOption) (*SendBatchResponse, error) {
	if emails == nil {
		emails = []SendEmailParams{}
	}
	out := new(SendBatchResponse)
	body := map[string]any{"emails": emails}
	return out, c.do(ctx, request{method: http.MethodPost, path: "/emails/batch", body: body, out: out}, opts)
}

// --------------------------------------------------------------- domains

// DomainsService manages sending and receiving domains.
type DomainsService struct{ c *Client }

// List returns every domain.
func (s *DomainsService) List(ctx context.Context, opts ...RequestOption) ([]Domain, error) {
	var out []Domain
	return out, s.c.do(ctx, request{method: http.MethodGet, path: "/domains", out: &out}, opts)
}

// Create registers a sending domain. Next: Records, publish them, then Verify.
func (s *DomainsService) Create(ctx context.Context, params *CreateDomainParams, opts ...RequestOption) (*Domain, error) {
	out := new(Domain)
	return out, s.c.do(ctx, request{method: http.MethodPost, path: "/domains", body: params, out: out}, opts)
}

// Records returns the DNS records to publish, each with its current status.
func (s *DomainsService) Records(ctx context.Context, id string, opts ...RequestOption) (*DomainRecords, error) {
	out := new(DomainRecords)
	return out, s.c.do(ctx, request{method: http.MethodGet, path: "/domains/" + seg(id) + "/records", out: out}, opts)
}

// Verify checks DNS now instead of waiting for the background check.
func (s *DomainsService) Verify(ctx context.Context, id string, opts ...RequestOption) (*Domain, error) {
	return s.post(ctx, "/domains/"+seg(id)+"/verify", nil, opts)
}

// EnableReceiving starts receiving mail on the domain. It adds receiving
// records and never blocks sending.
func (s *DomainsService) EnableReceiving(ctx context.Context, id string, opts ...RequestOption) (*Domain, error) {
	return s.post(ctx, "/domains/"+seg(id)+"/receiving", nil, opts)
}

// DisableReceiving stops receiving mail on the domain.
func (s *DomainsService) DisableReceiving(ctx context.Context, id string, opts ...RequestOption) (*Domain, error) {
	out := new(Domain)
	return out, s.c.do(ctx, request{method: http.MethodDelete, path: "/domains/" + seg(id) + "/receiving", out: out}, opts)
}

// SetRegion moves a domain to another region. Refused (region_immutable)
// once it holds messages.
func (s *DomainsService) SetRegion(ctx context.Context, id string, region Region, opts ...RequestOption) (*Domain, error) {
	out := new(Domain)
	body := map[string]any{"region": region}
	return out, s.c.do(ctx, request{method: http.MethodPatch, path: "/domains/" + seg(id) + "/region", body: body, out: out}, opts)
}

// DeletionPreview says what deleting the domain would take with it.
func (s *DomainsService) DeletionPreview(ctx context.Context, id string, opts ...RequestOption) (map[string]any, error) {
	var out map[string]any
	return out, s.c.do(ctx, request{method: http.MethodGet, path: "/domains/" + seg(id) + "/deletion", out: &out}, opts)
}

// Remove deletes a domain.
func (s *DomainsService) Remove(ctx context.Context, id string, opts ...RequestOption) error {
	return s.c.do(ctx, request{method: http.MethodDelete, path: "/domains/" + seg(id)}, opts)
}

func (s *DomainsService) post(ctx context.Context, path string, body any, opts []RequestOption) (*Domain, error) {
	out := new(Domain)
	return out, s.c.do(ctx, request{method: http.MethodPost, path: path, body: body, out: out}, opts)
}

// ------------------------------------------------------------- templates

// TemplatesService manages email templates.
type TemplatesService struct {
	c      *Client
	Tags   *TemplateTagsService
	Assets *TemplateAssetsService
}

// List returns the templates, optionally filtered.
func (s *TemplatesService) List(ctx context.Context, params *ListTemplatesParams, opts ...RequestOption) ([]TemplateSummary, error) {
	p := deref(params)
	var out []TemplateSummary
	qs := q().str("tag", p.Tag).str("project", p.Project)
	return out, s.c.do(ctx, request{method: http.MethodGet, path: "/templates", query: qs.values(), out: &out}, opts)
}

// Get returns a template by uuid or public id (tpl_...).
func (s *TemplatesService) Get(ctx context.Context, id string, opts ...RequestOption) (*Template, error) {
	out := new(Template)
	return out, s.c.do(ctx, request{method: http.MethodGet, path: "/templates/" + seg(id), out: out}, opts)
}

// HTML returns the rendered HTML, tags left in place.
func (s *TemplatesService) HTML(ctx context.Context, id string, opts ...RequestOption) (string, error) {
	var out string
	return out, s.c.do(ctx, request{method: http.MethodGet, path: "/templates/" + seg(id) + "/html", as: "text", out: &out}, opts)
}

// Create makes a new draft. Publish it before sending with it. params may be nil.
func (s *TemplatesService) Create(ctx context.Context, params *CreateTemplateParams, opts ...RequestOption) (*Template, error) {
	if params == nil {
		params = &CreateTemplateParams{}
	}
	out := new(Template)
	return out, s.c.do(ctx, request{method: http.MethodPost, path: "/templates", body: params, out: out}, opts)
}

// Update changes the fields set in params.
func (s *TemplatesService) Update(ctx context.Context, id string, params *UpdateTemplateParams, opts ...RequestOption) (*Template, error) {
	out := new(Template)
	return out, s.c.do(ctx, request{method: http.MethodPatch, path: "/templates/" + seg(id), body: params, out: out}, opts)
}

// Remove deletes a template.
func (s *TemplatesService) Remove(ctx context.Context, id string, opts ...RequestOption) error {
	return s.c.do(ctx, request{method: http.MethodDelete, path: "/templates/" + seg(id)}, opts)
}

// Duplicate copies a template as a new draft.
func (s *TemplatesService) Duplicate(ctx context.Context, id string, opts ...RequestOption) (*Template, error) {
	return s.action(ctx, id, "duplicate", opts)
}

// Publish makes a template sendable.
func (s *TemplatesService) Publish(ctx context.Context, id string, opts ...RequestOption) (*Template, error) {
	return s.action(ctx, id, "publish", opts)
}

// Unpublish returns a template to draft.
func (s *TemplatesService) Unpublish(ctx context.Context, id string, opts ...RequestOption) (*Template, error) {
	return s.action(ctx, id, "unpublish", opts)
}

// Preview renders with variables without sending anything. variables may be nil.
func (s *TemplatesService) Preview(ctx context.Context, id string, variables map[string]any, opts ...RequestOption) (*TemplatePreview, error) {
	if variables == nil {
		variables = map[string]any{}
	}
	out := new(TemplatePreview)
	body := map[string]any{"variables": variables}
	return out, s.c.do(ctx, request{method: http.MethodPost, path: "/templates/" + seg(id) + "/preview", body: body, out: out}, opts)
}

// SetTags replaces the template's tags with these tag ids.
func (s *TemplatesService) SetTags(ctx context.Context, id string, tagIDs []string, opts ...RequestOption) ([]TemplateTag, error) {
	if tagIDs == nil {
		tagIDs = []string{}
	}
	var out struct {
		Tags []TemplateTag `json:"tags"`
	}
	body := map[string]any{"tagIds": tagIDs}
	err := s.c.do(ctx, request{method: http.MethodPut, path: "/templates/" + seg(id) + "/tags", body: body, out: &out}, opts)
	return out.Tags, err
}

// Projects returns the projects (domains) templates can belong to.
func (s *TemplatesService) Projects(ctx context.Context, opts ...RequestOption) ([]NamedRef, error) {
	var out []NamedRef
	return out, s.c.do(ctx, request{method: http.MethodGet, path: "/templates/projects", out: &out}, opts)
}

func (s *TemplatesService) action(ctx context.Context, id, verb string, opts []RequestOption) (*Template, error) {
	out := new(Template)
	return out, s.c.do(ctx, request{method: http.MethodPost, path: "/templates/" + seg(id) + "/" + verb, out: out}, opts)
}

// TemplateTagsService manages template tags.
type TemplateTagsService struct{ c *Client }

// List returns every tag, each with its TemplateCount.
func (s *TemplateTagsService) List(ctx context.Context, opts ...RequestOption) ([]TemplateTag, error) {
	var out []TemplateTag
	return out, s.c.do(ctx, request{method: http.MethodGet, path: "/templates/tags", out: &out}, opts)
}

// Create makes a tag. color is gray, red, orange, amber, green, teal, blue, purple or pink.
func (s *TemplateTagsService) Create(ctx context.Context, name, color string, opts ...RequestOption) (*TemplateTag, error) {
	out := new(TemplateTag)
	body := map[string]any{"name": name, "color": color}
	return out, s.c.do(ctx, request{method: http.MethodPost, path: "/templates/tags", body: body, out: out}, opts)
}

// Update renames or recolours a tag; an empty string leaves that field alone.
func (s *TemplateTagsService) Update(ctx context.Context, id, name, color string, opts ...RequestOption) (*TemplateTag, error) {
	body := map[string]any{}
	if name != "" {
		body["name"] = name
	}
	if color != "" {
		body["color"] = color
	}
	out := new(TemplateTag)
	return out, s.c.do(ctx, request{method: http.MethodPatch, path: "/templates/tags/" + seg(id), body: body, out: out}, opts)
}

// Remove deletes a tag and returns how many templates it was taken off.
func (s *TemplateTagsService) Remove(ctx context.Context, id string, opts ...RequestOption) (int, error) {
	var out struct {
		TemplateCount int `json:"templateCount"`
	}
	err := s.c.do(ctx, request{method: http.MethodDelete, path: "/templates/tags/" + seg(id), out: &out}, opts)
	return out.TemplateCount, err
}

// TemplateAssetsService manages images templates can show.
type TemplateAssetsService struct{ c *Client }

// List returns the uploaded images.
func (s *TemplateAssetsService) List(ctx context.Context, opts ...RequestOption) ([]TemplateAsset, error) {
	var out []TemplateAsset
	return out, s.c.do(ctx, request{method: http.MethodGet, path: "/templates/assets", out: &out}, opts)
}

// Upload stores an image and returns its public URL.
func (s *TemplateAssetsService) Upload(ctx context.Context, params *UploadTemplateAssetParams, opts ...RequestOption) (*TemplateAsset, error) {
	body := map[string]any{"mimeType": params.MimeType, "base64": base64.StdEncoding.EncodeToString(params.Data)}
	if params.Name != "" {
		body["name"] = params.Name
	}
	out := new(TemplateAsset)
	return out, s.c.do(ctx, request{method: http.MethodPost, path: "/templates/assets", body: body, out: out}, opts)
}

// ------------------------------------------------------------- sandboxes

// SandboxesService manages sandboxes: inboxes that trap mail while you develop.
type SandboxesService struct {
	c        *Client
	Messages *SandboxMessagesService
}

// List returns every sandbox.
func (s *SandboxesService) List(ctx context.Context, opts ...RequestOption) ([]Sandbox, error) {
	var out []Sandbox
	return out, s.c.do(ctx, request{method: http.MethodGet, path: "/sandboxes", out: &out}, opts)
}

// Get returns one sandbox.
func (s *SandboxesService) Get(ctx context.Context, id string, opts ...RequestOption) (*Sandbox, error) {
	out := new(Sandbox)
	return out, s.c.do(ctx, request{method: http.MethodGet, path: "/sandboxes/" + seg(id), out: out}, opts)
}

// Create makes a sandbox. Name is required.
func (s *SandboxesService) Create(ctx context.Context, params *SandboxParams, opts ...RequestOption) (*Sandbox, error) {
	out := new(Sandbox)
	return out, s.c.do(ctx, request{method: http.MethodPost, path: "/sandboxes", body: params, out: out}, opts)
}

// Update renames a sandbox. Renaming never changes the address or the credentials.
func (s *SandboxesService) Update(ctx context.Context, id string, params *SandboxParams, opts ...RequestOption) (*Sandbox, error) {
	out := new(Sandbox)
	return out, s.c.do(ctx, request{method: http.MethodPatch, path: "/sandboxes/" + seg(id), body: params, out: out}, opts)
}

// Remove deletes the sandbox and every message in it, and returns how many messages went.
func (s *SandboxesService) Remove(ctx context.Context, id string, opts ...RequestOption) (int, error) {
	var out struct {
		DeletedMessages int `json:"deletedMessages"`
	}
	err := s.c.do(ctx, request{method: http.MethodDelete, path: "/sandboxes/" + seg(id), out: &out}, opts)
	return out.DeletedMessages, err
}

// Credentials returns the SMTP host, port, username and password to point a project at.
func (s *SandboxesService) Credentials(ctx context.Context, id string, opts ...RequestOption) (*SandboxCredentials, error) {
	out := new(SandboxCredentials)
	return out, s.c.do(ctx, request{method: http.MethodGet, path: "/sandboxes/" + seg(id) + "/credentials", out: out}, opts)
}

// ResetCredentials replaces BOTH the SMTP password and the sc_sbx_ token.
// The token is shown only here.
func (s *SandboxesService) ResetCredentials(ctx context.Context, id string, opts ...RequestOption) (*ResetSandboxCredentials, error) {
	out := new(ResetSandboxCredentials)
	return out, s.c.do(ctx, request{method: http.MethodPost, path: "/sandboxes/" + seg(id) + "/credentials", out: out}, opts)
}

// Send sends to a sandbox through its JSON door, with the sandbox's own
// sc_sbx_ token. Same fields as Emails.Send; nothing is delivered.
func (s *SandboxesService) Send(ctx context.Context, sandboxToken string, params *SendEmailParams, opts ...RequestOption) (*SandboxSendResult, error) {
	out := new(SandboxSendResult)
	return out, s.c.do(ctx, request{method: http.MethodPost, path: "/sandbox/emails", body: params, bearer: sandboxToken, out: out}, opts)
}

// SandboxMessagesService reads what a sandbox trapped.
type SandboxMessagesService struct{ c *Client }

// List returns a sandbox's messages, newest first.
func (s *SandboxMessagesService) List(ctx context.Context, sandboxID string, params *ListSandboxMessagesParams, opts ...RequestOption) ([]SandboxMessageSummary, error) {
	p := deref(params)
	var out []SandboxMessageSummary
	qs := q().str("search", p.Search).num("limit", p.Limit)
	return out, s.c.do(ctx, request{method: http.MethodGet, path: "/sandboxes/" + seg(sandboxID) + "/messages", query: qs.values(), out: &out}, opts)
}

// Get returns one trapped message in full.
func (s *SandboxMessagesService) Get(ctx context.Context, sandboxID, messageID string, opts ...RequestOption) (*SandboxMessage, error) {
	out := new(SandboxMessage)
	return out, s.c.do(ctx, request{method: http.MethodGet, path: "/sandboxes/" + seg(sandboxID) + "/messages/" + seg(messageID), out: out}, opts)
}

// Raw returns the message's raw MIME source.
func (s *SandboxMessagesService) Raw(ctx context.Context, sandboxID, messageID string, opts ...RequestOption) (string, error) {
	var out string
	path := "/sandboxes/" + seg(sandboxID) + "/messages/" + seg(messageID) + "/raw"
	return out, s.c.do(ctx, request{method: http.MethodGet, path: path, as: "text", out: &out}, opts)
}

// Remove deletes one trapped message.
func (s *SandboxMessagesService) Remove(ctx context.Context, sandboxID, messageID string, opts ...RequestOption) error {
	return s.c.do(ctx, request{method: http.MethodDelete, path: "/sandboxes/" + seg(sandboxID) + "/messages/" + seg(messageID)}, opts)
}

// Clear empties the sandbox and returns how many messages went.
func (s *SandboxMessagesService) Clear(ctx context.Context, sandboxID string, opts ...RequestOption) (int, error) {
	var out struct {
		Deleted int `json:"deleted"`
	}
	err := s.c.do(ctx, request{method: http.MethodDelete, path: "/sandboxes/" + seg(sandboxID) + "/messages", out: &out}, opts)
	return out.Deleted, err
}

// MarkAllRead marks every message read and returns how many changed.
func (s *SandboxMessagesService) MarkAllRead(ctx context.Context, sandboxID string, opts ...RequestOption) (int, error) {
	var out struct {
		Updated int `json:"updated"`
	}
	err := s.c.do(ctx, request{method: http.MethodPost, path: "/sandboxes/" + seg(sandboxID) + "/read-all", out: &out}, opts)
	return out.Updated, err
}

// WaitForOptions tunes WaitFor.
type WaitForOptions struct {
	// Match picks the message to wait for. Default: any message trapped after the call started.
	Match func(SandboxMessageSummary) bool
	// Interval between polls. Default 1 second.
	Interval time.Duration
}

// WaitFor polls until a message arrives that opts.Match accepts, then
// returns it in full. Handy in end-to-end tests. Bound it with ctx: when ctx
// ends first the error's Name is "timeout".
func (s *SandboxMessagesService) WaitFor(ctx context.Context, sandboxID string, opts *WaitForOptions) (*SandboxMessage, error) {
	o := deref(opts)
	started := time.Now()
	if o.Match == nil {
		o.Match = func(m SandboxMessageSummary) bool { return !m.TrappedAt.Before(started.Add(-time.Second)) }
	}
	if o.Interval <= 0 {
		o.Interval = time.Second
	}
	for {
		page, err := s.List(ctx, sandboxID, &ListSandboxMessagesParams{Limit: 50})
		if err != nil {
			if ctx.Err() != nil {
				return nil, waitTimeout(sandboxID, started)
			}
			return nil, err
		}
		for _, m := range page {
			if o.Match(m) {
				return s.Get(ctx, sandboxID, m.ID)
			}
		}
		if sleepCtx(ctx, o.Interval) != nil {
			return nil, waitTimeout(sandboxID, started)
		}
	}
}

func waitTimeout(sandboxID string, started time.Time) *Error {
	return &Error{Name: "timeout", Message: fmt.Sprintf("No matching message reached sandbox %s within %s.", sandboxID, time.Since(started).Round(time.Millisecond))}
}

// -------------------------------------------------------------- webhooks

// WebhooksService manages webhook endpoints.
type WebhooksService struct {
	c          *Client
	Dispatches *WebhookDispatchesService
}

// List returns every endpoint.
func (s *WebhooksService) List(ctx context.Context, opts ...RequestOption) ([]WebhookEndpoint, error) {
	var out []WebhookEndpoint
	return out, s.c.do(ctx, request{method: http.MethodGet, path: "/webhook-endpoints", out: &out}, opts)
}

// Create registers an endpoint. The response carries the signing Secret once: store it.
func (s *WebhooksService) Create(ctx context.Context, params *CreateWebhookParams, opts ...RequestOption) (*WebhookEndpointWithSecret, error) {
	out := new(WebhookEndpointWithSecret)
	return out, s.c.do(ctx, request{method: http.MethodPost, path: "/webhook-endpoints", body: params, out: out}, opts)
}

// Update changes the fields set in params.
func (s *WebhooksService) Update(ctx context.Context, id string, params *UpdateWebhookParams, opts ...RequestOption) (*WebhookEndpoint, error) {
	out := new(WebhookEndpoint)
	return out, s.c.do(ctx, request{method: http.MethodPatch, path: "/webhook-endpoints/" + seg(id), body: params, out: out}, opts)
}

// Remove deletes an endpoint.
func (s *WebhooksService) Remove(ctx context.Context, id string, opts ...RequestOption) error {
	return s.c.do(ctx, request{method: http.MethodDelete, path: "/webhook-endpoints/" + seg(id)}, opts)
}

// RotateSecret makes a new secret. The old one keeps signing alongside it for the grace window.
func (s *WebhooksService) RotateSecret(ctx context.Context, id string, opts ...RequestOption) (*WebhookEndpointWithSecret, error) {
	out := new(WebhookEndpointWithSecret)
	return out, s.c.do(ctx, request{method: http.MethodPost, path: "/webhook-endpoints/" + seg(id) + "/rotate-secret", out: out}, opts)
}

// Deliveries returns recent delivery attempts.
func (s *WebhooksService) Deliveries(ctx context.Context, params *ListWebhookDeliveriesParams, opts ...RequestOption) ([]map[string]any, error) {
	p := deref(params)
	var out []map[string]any
	qs := q().str("endpointId", p.EndpointID).flag("finalOnly", p.FinalOnly).num("limit", p.Limit)
	return out, s.c.do(ctx, request{method: http.MethodGet, path: "/webhook-deliveries", query: qs.values(), out: &out}, opts)
}

// Verify is [VerifyWebhook]. It makes no request.
func (s *WebhooksService) Verify(payload []byte, headers http.Header, secret string, opts ...VerifyOption) (*WebhookEvent, error) {
	return VerifyWebhook(payload, headers, secret, opts...)
}

// WebhookDispatchesService reads and replays undelivered events.
type WebhookDispatchesService struct{ c *Client }

// List returns undelivered events: dead letters by default, or State "retrying".
func (s *WebhookDispatchesService) List(ctx context.Context, params *ListWebhookDispatchesParams, opts ...RequestOption) (*WebhookDispatchPage, error) {
	p := deref(params)
	out := new(WebhookDispatchPage)
	qs := q().str("state", p.State).str("endpointId", p.EndpointID).str("cursor", p.Cursor).num("limit", p.Limit)
	return out, s.c.do(ctx, request{method: http.MethodGet, path: "/webhook-dispatches", query: qs.values(), out: out}, opts)
}

// All walks every dispatch matching params, following the cursor.
func (s *WebhookDispatchesService) All(ctx context.Context, params *ListWebhookDispatchesParams, opts ...RequestOption) iter.Seq2[WebhookDispatch, error] {
	p := deref(params)
	p.Cursor = ""
	return walk(func() ([]WebhookDispatch, CursorInfo, error) {
		res, err := s.List(ctx, &p, opts...)
		if err != nil {
			return nil, CursorInfo{}, err
		}
		if res.Page.Cursor != nil {
			p.Cursor = *res.Page.Cursor
		}
		return res.Data, res.Page, nil
	})
}

// Get returns one dispatch.
func (s *WebhookDispatchesService) Get(ctx context.Context, id string, opts ...RequestOption) (*WebhookDispatch, error) {
	out := new(WebhookDispatch)
	return out, s.c.do(ctx, request{method: http.MethodGet, path: "/webhook-dispatches/" + seg(id), out: out}, opts)
}

// Replay makes one more immediate attempt for a dead-lettered event.
func (s *WebhookDispatchesService) Replay(ctx context.Context, id string, opts ...RequestOption) (*WebhookDispatch, error) {
	out := new(WebhookDispatch)
	return out, s.c.do(ctx, request{method: http.MethodPost, path: "/webhook-dispatches/" + seg(id) + "/replay", out: out}, opts)
}

// ReplayAll replays every dead letter (optionally of one endpoint), up to the server's cap. params may be nil.
func (s *WebhookDispatchesService) ReplayAll(ctx context.Context, params *ReplayAllParams, opts ...RequestOption) (*ReplayAllResult, error) {
	if params == nil {
		params = &ReplayAllParams{}
	}
	out := new(ReplayAllResult)
	return out, s.c.do(ctx, request{method: http.MethodPost, path: "/webhook-dispatches/replay-all", body: params, out: out}, opts)
}

// ------------------------------------------------- contacts & audiences

// ContactsService manages contacts.
type ContactsService struct{ c *Client }

// List returns one page of contacts.
func (s *ContactsService) List(ctx context.Context, params *ListContactsParams, opts ...RequestOption) (*ContactList, error) {
	p := deref(params)
	out := new(ContactList)
	qs := q().str("audienceId", p.AudienceID).str("status", p.Status).num("page", p.Page).num("pageSize", p.PageSize)
	return out, s.c.do(ctx, request{method: http.MethodGet, path: "/contacts", query: qs.values(), out: out}, opts)
}

// All walks every contact, page after page.
func (s *ContactsService) All(ctx context.Context, params *ListContactsParams, opts ...RequestOption) iter.Seq2[Contact, error] {
	p := deref(params)
	if p.PageSize == 0 {
		p.PageSize = 200
	}
	return func(yield func(Contact, error) bool) {
		for page := 1; ; page++ {
			p.Page = page
			res, err := s.List(ctx, &p, opts...)
			if err != nil {
				yield(Contact{}, err)
				return
			}
			for _, c := range res.Items {
				if !yield(c, nil) {
					return
				}
			}
			if page >= res.TotalPages || len(res.Items) == 0 {
				return
			}
		}
	}
}

// Create adds a contact.
func (s *ContactsService) Create(ctx context.Context, params *CreateContactParams, opts ...RequestOption) (*Contact, error) {
	out := new(Contact)
	return out, s.c.do(ctx, request{method: http.MethodPost, path: "/contacts", body: params, out: out}, opts)
}

// Import imports a CSV or vCard file. Importing never suppresses anyone.
func (s *ContactsService) Import(ctx context.Context, params *ImportContactsParams, opts ...RequestOption) (map[string]any, error) {
	return s.upload(ctx, "/contacts/import", params, opts)
}

// ImportPreview says what an import would do, without importing.
func (s *ContactsService) ImportPreview(ctx context.Context, params *ImportContactsParams, opts ...RequestOption) (map[string]any, error) {
	return s.upload(ctx, "/contacts/import/preview", params, opts)
}

// Export returns every contact as CSV (re-importable) or vCard 4.0 (Format "vcf").
func (s *ContactsService) Export(ctx context.Context, params *ExportContactsParams, opts ...RequestOption) (string, error) {
	p := deref(params)
	var out string
	qs := q().str("format", p.Format).str("audience", p.Audience)
	return out, s.c.do(ctx, request{method: http.MethodGet, path: "/contacts/export", query: qs.values(), as: "text", out: &out}, opts)
}

func (s *ContactsService) upload(ctx context.Context, path string, params *ImportContactsParams, opts []RequestOption) (map[string]any, error) {
	form, ctype, err := importForm(params)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	return out, s.c.do(ctx, request{method: http.MethodPost, path: path, form: form, formType: ctype, out: &out}, opts)
}

func importForm(p *ImportContactsParams) ([]byte, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	field := func(name, value string) {
		if value != "" {
			_ = w.WriteField(name, value)
		}
	}
	field("audienceId", p.AudienceID)
	field("newAudience", p.NewAudience)
	if p.CreateAudiences != nil {
		field("createAudiences", fmt.Sprint(*p.CreateAudiences))
	}
	field("onDuplicate", p.OnDuplicate)
	if len(p.Mapping) > 0 {
		b, err := json.Marshal(p.Mapping)
		if err != nil {
			return nil, "", err
		}
		field("mapping", string(b))
	}
	name := p.Filename
	if name == "" {
		name = "contacts.csv"
	}
	ctype := "text/csv"
	if strings.HasSuffix(strings.ToLower(name), ".vcf") {
		ctype = "text/vcard"
	}
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\r", " ", "\n", " ").Replace(name)))
	h.Set("Content-Type", ctype)
	part, err := w.CreatePart(h)
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(p.File); err != nil {
		return nil, "", err
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), w.FormDataContentType(), nil
}

// AudiencesService lists audiences.
type AudiencesService struct{ c *Client }

// List returns every audience and the total contact count.
func (s *AudiencesService) List(ctx context.Context, opts ...RequestOption) (*AudienceList, error) {
	out := new(AudienceList)
	return out, s.c.do(ctx, request{method: http.MethodGet, path: "/audiences", out: out}, opts)
}

// SuppressionsService manages the suppression list.
type SuppressionsService struct{ c *Client }

// List returns one cursor page of the list.
func (s *SuppressionsService) List(ctx context.Context, params *ListSuppressionsParams, opts ...RequestOption) (*SuppressionList, error) {
	p := deref(params)
	out := new(SuppressionList)
	qs := q().str("q", p.Q).str("reason", p.Reason).str("cursor", p.Cursor).num("limit", p.Limit)
	return out, s.c.do(ctx, request{method: http.MethodGet, path: "/suppressions", query: qs.values(), out: out}, opts)
}

// All walks every suppression matching params, following the cursor.
func (s *SuppressionsService) All(ctx context.Context, params *ListSuppressionsParams, opts ...RequestOption) iter.Seq2[Suppression, error] {
	p := deref(params)
	p.Cursor = ""
	return walk(func() ([]Suppression, CursorInfo, error) {
		res, err := s.List(ctx, &p, opts...)
		if err != nil {
			return nil, CursorInfo{}, err
		}
		if res.Page.Cursor != nil {
			p.Cursor = *res.Page.Cursor
		}
		return res.Data, res.Page, nil
	})
}

// Create stops all mail to an address (reason manual).
func (s *SuppressionsService) Create(ctx context.Context, params *CreateSuppressionParams, opts ...RequestOption) (*SuppressionCreated, error) {
	out := new(SuppressionCreated)
	return out, s.c.do(ctx, request{method: http.MethodPost, path: "/suppressions", body: params, out: out}, opts)
}

// Remove lifts a suppression. reason (10+ characters) is recorded. Complaint
// and provider suppressions cannot be removed (suppression_not_removable).
func (s *SuppressionsService) Remove(ctx context.Context, id, reason string, opts ...RequestOption) error {
	body := map[string]any{"reason": reason}
	return s.c.do(ctx, request{method: http.MethodDelete, path: "/suppressions/" + seg(id), body: body}, opts)
}

// Export returns the list as CSV. Cursor and Limit are ignored.
func (s *SuppressionsService) Export(ctx context.Context, params *ListSuppressionsParams, opts ...RequestOption) (string, error) {
	p := deref(params)
	var out string
	qs := q().str("q", p.Q).str("reason", p.Reason)
	return out, s.c.do(ctx, request{method: http.MethodGet, path: "/suppressions/export", query: qs.values(), as: "text", out: &out}, opts)
}

// ------------------------------------------------------------- mailboxes

// MailboxesService manages addresses on receiving domains.
type MailboxesService struct {
	c        *Client
	Messages *MailboxMessagesService
}

// List returns the addresses; includeArchived lists archived ones too.
func (s *MailboxesService) List(ctx context.Context, includeArchived bool, opts ...RequestOption) ([]Mailbox, error) {
	var out []Mailbox
	qs := q()
	if includeArchived {
		qs = qs.str("includeArchived", "true")
	}
	return out, s.c.do(ctx, request{method: http.MethodGet, path: "/mailboxes", query: qs.values(), out: &out}, opts)
}

// Get returns one address.
func (s *MailboxesService) Get(ctx context.Context, id string, opts ...RequestOption) (*Mailbox, error) {
	out := new(Mailbox)
	return out, s.c.do(ctx, request{method: http.MethodGet, path: "/mailboxes/" + seg(id), out: out}, opts)
}

// Create makes a new address on a receiving domain.
func (s *MailboxesService) Create(ctx context.Context, params *CreateMailboxParams, opts ...RequestOption) (*Mailbox, error) {
	out := new(Mailbox)
	return out, s.c.do(ctx, request{method: http.MethodPost, path: "/mailboxes", body: params, out: out}, opts)
}

// Update changes the fields set in params.
func (s *MailboxesService) Update(ctx context.Context, id string, params *UpdateMailboxParams, opts ...RequestOption) (*Mailbox, error) {
	out := new(Mailbox)
	return out, s.c.do(ctx, request{method: http.MethodPatch, path: "/mailboxes/" + seg(id), body: params, out: out}, opts)
}

// Archive archives the address. Its mail is kept.
func (s *MailboxesService) Archive(ctx context.Context, id string, opts ...RequestOption) (*Mailbox, error) {
	out := new(Mailbox)
	return out, s.c.do(ctx, request{method: http.MethodDelete, path: "/mailboxes/" + seg(id), out: out}, opts)
}

// MailboxMessagesService reads mail filed under an address. Reading never marks anything read.
type MailboxMessagesService struct{ c *Client }

// List returns one cursor page of an address's mail.
func (s *MailboxMessagesService) List(ctx context.Context, mailboxID string, params *ListMailboxMessagesParams, opts ...RequestOption) (*MailboxMessageList, error) {
	p := deref(params)
	out := new(MailboxMessageList)
	qs := q().str("folder", p.Folder).str("search", p.Search).str("label", p.Label).num("limit", p.Limit).str("cursor", p.Cursor)
	return out, s.c.do(ctx, request{method: http.MethodGet, path: "/mailboxes/" + seg(mailboxID) + "/messages", query: qs.values(), out: out}, opts)
}

// All walks an address's mail, following the cursor.
func (s *MailboxMessagesService) All(ctx context.Context, mailboxID string, params *ListMailboxMessagesParams, opts ...RequestOption) iter.Seq2[MailboxMessageRow, error] {
	p := deref(params)
	p.Cursor = ""
	return walk(func() ([]MailboxMessageRow, CursorInfo, error) {
		res, err := s.List(ctx, mailboxID, &p, opts...)
		if err != nil {
			return nil, CursorInfo{}, err
		}
		if res.Page.Cursor != nil {
			p.Cursor = *res.Page.Cursor
		}
		return res.Items, res.Page, nil
	})
}

// Get returns one message in full, with its conversation.
func (s *MailboxMessagesService) Get(ctx context.Context, mailboxID, messageID string, opts ...RequestOption) (*MailboxMessage, error) {
	out := new(MailboxMessage)
	return out, s.c.do(ctx, request{method: http.MethodGet, path: "/mailboxes/" + seg(mailboxID) + "/messages/" + seg(messageID), out: out}, opts)
}

// Attachment downloads one attachment.
func (s *MailboxMessagesService) Attachment(ctx context.Context, mailboxID, messageID, attachmentID string, opts ...RequestOption) (*DownloadedFile, error) {
	out := new(DownloadedFile)
	path := "/mailboxes/" + seg(mailboxID) + "/messages/" + seg(messageID) + "/parts/" + seg(attachmentID)
	return out, s.c.do(ctx, request{method: http.MethodGet, path: path, query: q().str("download", "1").values(), as: "bytes", out: out}, opts)
}

// ------------------------------------------------------------- workspace

// WorkspaceService reads the key's workspace.
type WorkspaceService struct{ c *Client }

// Get returns the workspace this key belongs to.
func (s *WorkspaceService) Get(ctx context.Context, opts ...RequestOption) (*Workspace, error) {
	out := new(Workspace)
	return out, s.c.do(ctx, request{method: http.MethodGet, path: "/workspace", out: out}, opts)
}

// APIKeysService lists API keys. Keys are created and revoked in the dashboard only.
type APIKeysService struct{ c *Client }

// List returns the workspace's keys, masked.
func (s *APIKeysService) List(ctx context.Context, opts ...RequestOption) ([]APIKey, error) {
	var out []APIKey
	return out, s.c.do(ctx, request{method: http.MethodGet, path: "/api-keys", out: &out}, opts)
}

// AutomationsService starts automations.
type AutomationsService struct{ c *Client }

// Enroll starts a Live automation with a manual trigger for one contact.
func (s *AutomationsService) Enroll(ctx context.Context, automationID, email string, opts ...RequestOption) (map[string]any, error) {
	var out map[string]any
	body := map[string]any{"email": email}
	return out, s.c.do(ctx, request{method: http.MethodPost, path: "/automations/" + seg(automationID) + "/enrollments", body: body, out: &out}, opts)
}

// ------------------------------------------------------------------ util

func deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}

// walk turns a cursor-page fetcher into an iterator. fetch advances its own cursor.
func walk[T any](fetch func() ([]T, CursorInfo, error)) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		for {
			items, page, err := fetch()
			if err != nil {
				var zero T
				yield(zero, err)
				return
			}
			for _, it := range items {
				if !yield(it, nil) {
					return
				}
			}
			if !page.HasMore || page.Cursor == nil || *page.Cursor == "" {
				return
			}
		}
	}
}
