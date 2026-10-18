package sendclipper

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"time"
)

// The request and response shapes of the SendClipper REST API (/v1).
//
// The API only ever ADDS fields inside /v1; it never removes or retypes one.
// Fields a response carries that are not listed here are ignored.

// ---------------------------------------------------------------- shared

// CursorInfo is the paging block of a cursor-addressed list.
type CursorInfo struct {
	Cursor  *string `json:"cursor"`
	HasMore bool    `json:"hasMore"`
}

// NamedRef is a {id, name} pair.
type NamedRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// SendClass is "transactional" (the default) or "marketing".
type SendClass string

const (
	Transactional SendClass = "transactional"
	Marketing     SendClass = "marketing"
)

// ---------------------------------------------------------------- emails

// SendEmailParams is one message. Give it a body (Subject + HTML) or a
// published template (TemplateID or Template, plus Variables).
type SendEmailParams struct {
	// From is a bare address on a verified domain: "hello@send.acme.io". A display-name form is refused.
	From string `json:"from"`
	// To is ONE recipient. For several, use Batch.Send with one item each.
	To      string `json:"to"`
	Subject string `json:"subject,omitempty"`
	HTML    string `json:"html,omitempty"`
	// Text is the plain-text alternative.
	Text string `json:"text,omitempty"`
	// TemplateID is a published template's public id (tpl_...) or uuid.
	TemplateID string `json:"templateId,omitempty"`
	// Template is a published template's send name (its key, e.g. "welcome"). Use this or TemplateID.
	Template string `json:"template,omitempty"`
	// Variables fill {{tags}} in a template, or in a hand-written HTML body.
	Variables map[string]any `json:"variables,omitempty"`
	// SendClass "marketing" adds one-click unsubscribe and the postal-address footer.
	SendClass SendClass `json:"sendClass,omitempty"`
	// TrackOpens false leaves the open-tracking pixel out. Default true.
	TrackOpens *bool `json:"trackOpens,omitempty"`
	// TrackClicks false leaves links unwrapped. Default true.
	TrackClicks *bool `json:"trackClicks,omitempty"`
	// ReplyTo is the Reply-To header: 1 to 10 bare addresses. Accepted on a batch item too.
	ReplyTo []string `json:"replyTo,omitempty"`
	// Attachments are files sent with the message: 10 MiB per file, 25 MiB of
	// files per message, 20 files. Not accepted on a batch item: the API fails
	// that item with attachments_not_batchable.
	Attachments []Attachment `json:"attachments,omitempty"`
}

// Attachment is one file sent with a message.
type Attachment struct {
	// Filename is the name the recipient sees, e.g. "booking-42.pdf".
	Filename string `json:"filename"`
	// Content is the file's raw bytes; it is base64-encoded for you.
	Content []byte `json:"content"`
	// ContentType is the MIME type, e.g. "application/pdf". Empty lets the API guess it from Filename.
	ContentType string `json:"contentType,omitempty"`
}

// MarshalJSON writes Content as standard, padded base64 (RFC 4648 §4), which
// is what the API decodes. encoding/json would pick the same encoding for a
// []byte on its own; it is spelled out here so the wire format does not rest
// on that default. HTML escaping is off, as it is for the request body, so a
// filename like "Q&A.pdf" travels as written.
func (a Attachment) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	err := enc.Encode(struct {
		Filename    string `json:"filename"`
		Content     string `json:"content"`
		ContentType string `json:"contentType,omitempty"`
	}{a.Filename, base64.StdEncoding.EncodeToString(a.Content), a.ContentType})
	return bytes.TrimRight(buf.Bytes(), "\n"), err
}

// EmailStatus is a message's delivery state.
type EmailStatus string

const (
	StatusQueued     EmailStatus = "queued"
	StatusSent       EmailStatus = "sent"
	StatusDelivered  EmailStatus = "delivered"
	StatusOpened     EmailStatus = "opened"
	StatusClicked    EmailStatus = "clicked"
	StatusBounced    EmailStatus = "bounced"
	StatusComplained EmailStatus = "complained"
	StatusFailed     EmailStatus = "failed"
)

// SentEmail is what Emails.Send answers: the message was accepted (queued).
type SentEmail struct {
	// ID is SendClipper's id for the message. Use it with Emails.Get.
	ID           string      `json:"id"`
	MessageID    string      `json:"messageId"`
	SesMessageID *string     `json:"sesMessageId"`
	From         string      `json:"from"`
	To           string      `json:"to"`
	Domain       string      `json:"domain"`
	Subject      string      `json:"subject"`
	Status       EmailStatus `json:"status"`
	CreatedAt    time.Time   `json:"createdAt"`
	TemplateID   *string     `json:"templateId"`
}

// BatchItemError is why one batch item was refused.
type BatchItemError struct {
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Detail  json.RawMessage `json:"detail,omitempty"`
}

// BatchResult is one submitted item's outcome. Status is "accepted" (ID and
// MessageID set) or "failed" (Error set).
type BatchResult struct {
	Index     int             `json:"index"`
	Status    string          `json:"status"`
	ID        string          `json:"id,omitempty"`
	MessageID string          `json:"messageId,omitempty"`
	Error     *BatchItemError `json:"error,omitempty"`
}

// SendBatchResponse holds one result per submitted item, in order.
type SendBatchResponse struct {
	Data    []BatchResult `json:"data"`
	Summary struct {
		Submitted int `json:"submitted"`
		Accepted  int `json:"accepted"`
		Failed    int `json:"failed"`
	} `json:"summary"`
}

// ListEmailsParams filters the sent-mail log.
type ListEmailsParams struct {
	Search      string
	Status      EmailStatus
	Domain      string // exact sending domain
	BroadcastID string
	From        string // ISO 8601 lower bound
	To          string // ISO 8601 upper bound
	Page        int
	PageSize    int // 1-100, default 50
}

// EmailRow is one message in the log.
type EmailRow struct {
	ID           string      `json:"id"`
	MessageID    string      `json:"messageId"`
	From         string      `json:"from"`
	To           string      `json:"to"`
	Domain       string      `json:"domain"`
	Subject      string      `json:"subject"`
	Status       EmailStatus `json:"status"`
	SesMessageID *string     `json:"sesMessageId"`
	CreatedAt    time.Time   `json:"createdAt"`
}

// EmailList is one numbered page of the log.
type EmailList struct {
	Items    []EmailRow `json:"items"`
	Total    int        `json:"total"`
	Page     int        `json:"page"`
	PageSize int        `json:"pageSize"`
	// TotalCapped: a search counts only its newest 1,000 matches.
	TotalCapped bool `json:"totalCapped"`
}

// EmailEvent is one step of a message's timeline.
type EmailEvent struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Meta      json.RawMessage `json:"meta"`
	CreatedAt time.Time       `json:"createdAt"`
}

// EmailDetail is one message with its body and timeline.
type EmailDetail struct {
	EmailRow
	HTML *string `json:"html"`
	Text *string `json:"text"`
	// BodyExpired is true once the retention window deleted the body.
	BodyExpired bool         `json:"bodyExpired"`
	Events      []EmailEvent `json:"events"`
	ThreadID    *string      `json:"threadId"`
	SendClass   SendClass    `json:"sendClass"`
	TrackOpens  *bool        `json:"trackOpens"`
	TrackClicks *bool        `json:"trackClicks"`
}

// EmailStats counts messages per status.
type EmailStats struct {
	Total             int `json:"total"`
	Queued            int `json:"queued"`
	Sent              int `json:"sent"`
	Delivered         int `json:"delivered"`
	Opened            int `json:"opened"`
	Clicked           int `json:"clicked"`
	Bounced           int `json:"bounced"`
	Complained        int `json:"complained"`
	Failed            int `json:"failed"`
	BodyRetentionDays int `json:"bodyRetentionDays"`
}

// --------------------------------------------------------------- domains

// Region is where Amazon SES registers a domain.
type Region string

const (
	USEast1 Region = "us-east-1"
	EUWest1 Region = "eu-west-1"
	USWest2 Region = "us-west-2"
)

// Domain is a sending (and optionally receiving) domain.
type Domain struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Region  Region `json:"region"`
	Status  string `json:"status"` // verified, pending or failed
	SpfOK   bool   `json:"spfOk"`
	DkimOK  bool   `json:"dkimOk"`
	DmarcOK bool   `json:"dmarcOk"`
	Records []struct {
		Name string `json:"name"`
		OK   bool   `json:"ok"`
	} `json:"records"`
	Sent              int        `json:"sent"`
	CheckCount        int        `json:"checkCount"`
	LastCheckedAt     *time.Time `json:"lastCheckedAt"`
	CreatedAt         time.Time  `json:"createdAt"`
	LastError         *string    `json:"lastError"`
	ReceivingEnabled  bool       `json:"receivingEnabled"`
	MxOK              bool       `json:"mxOk"`
	ReceivingVerified bool       `json:"receivingVerified"`
	ReceivingError    *string    `json:"receivingError"`
}

// DNSRecord is one record to publish.
type DNSRecord struct {
	Type      string `json:"type"`
	Purpose   string `json:"purpose"`
	Host      string `json:"host"`
	Value     string `json:"value"`
	CopyValue string `json:"copyValue,omitempty"`
	Priority  int    `json:"priority,omitempty"`
	Status    string `json:"status,omitempty"`
}

// DomainRecords is a domain with the DNS records to publish.
type DomainRecords struct {
	Domain  Domain      `json:"domain"`
	Records []DNSRecord `json:"records"`
}

// CreateDomainParams registers a domain.
type CreateDomainParams struct {
	// Name is a hostname such as send.acme.com. No scheme, no trailing slash.
	Name string `json:"name"`
	// Region cannot change once the domain holds messages.
	Region Region `json:"region"`
}

// ------------------------------------------------------------- templates

// TemplateBlock is one block: {"type": "text", "text": "Hi {{first_name|there}}"}.
type TemplateBlock = map[string]any

// TemplateTag labels templates.
type TemplateTag struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Color         string `json:"color"`
	TemplateCount *int   `json:"templateCount,omitempty"`
}

// TemplateSummary is one card in the template list.
type TemplateSummary struct {
	ID          string        `json:"id"`
	PublicID    string        `json:"publicId"`
	Name        string        `json:"name"`
	Status      string        `json:"status"` // draft or published
	BlockCount  int           `json:"blockCount"`
	UpdatedAt   time.Time     `json:"updatedAt"`
	PublishedAt *time.Time    `json:"publishedAt"`
	CreatedAt   time.Time     `json:"createdAt"`
	Domains     []NamedRef    `json:"domains"`
	Tags        []TemplateTag `json:"tags"`
	Key         *string       `json:"key"`
	Project     *NamedRef     `json:"project"`
}

// Template is a template with its body.
type Template struct {
	TemplateSummary
	Subject   string          `json:"subject"`
	Preheader string          `json:"preheader"`
	Blocks    []TemplateBlock `json:"blocks"`
	HTML      string          `json:"html"`
}

// CreateTemplateParams makes a draft. Every field is optional.
type CreateTemplateParams struct {
	Name      string `json:"name,omitempty"`
	Subject   string `json:"subject,omitempty"`
	Preheader string `json:"preheader,omitempty"`
	// Blocks: see https://sendclipper.com/docs for the block types. Unknown fields are dropped.
	Blocks          []TemplateBlock `json:"blocks,omitempty"`
	ProjectDomainID string          `json:"projectDomainId,omitempty"`
	// Key is the send name, unique in its project: Emails.Send with Template: key.
	Key string `json:"key,omitempty"`
}

// UpdateTemplateParams changes the fields you set. Use a pointer to "" (or
// an empty slice) to clear one.
type UpdateTemplateParams struct {
	Name            *string         `json:"name,omitempty"`
	Subject         *string         `json:"subject,omitempty"`
	Preheader       *string         `json:"preheader,omitempty"`
	Blocks          []TemplateBlock `json:"blocks,omitempty"`
	DomainIDs       []string        `json:"domainIds,omitempty"`
	ProjectDomainID *string         `json:"projectDomainId,omitempty"`
	Key             *string         `json:"key,omitempty"`
}

// ListTemplatesParams filters the template list.
type ListTemplatesParams struct {
	Tag     string // a tag id
	Project string // a project (domain) id
}

// TemplatePreview is a render that sent nothing.
type TemplatePreview struct {
	Subject   string `json:"subject"`
	Preheader string `json:"preheader"`
	HTML      string `json:"html"`
	Text      string `json:"text"`
	Variables []struct {
		Name     string  `json:"name"`
		Fallback *string `json:"fallback"`
		Value    string  `json:"value"`
		Provided bool    `json:"provided"`
	} `json:"variables"`
	// UnsafeLinks are links whose target a tag would make unsafe.
	UnsafeLinks []string `json:"unsafeLinks"`
}

// TemplateAsset is an uploaded image.
type TemplateAsset struct {
	ID       string `json:"id"`
	PublicID string `json:"publicId"`
	// URL is public: a mail client can load it.
	URL       string    `json:"url"`
	MimeType  string    `json:"mimeType"`
	Bytes     int       `json:"bytes"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

// UploadTemplateAssetParams is one image.
type UploadTemplateAssetParams struct {
	// MimeType is image/png, image/jpeg, image/gif or image/webp.
	MimeType string
	// Data is the file's bytes; it is base64-encoded for you.
	Data []byte
	Name string
}

// ------------------------------------------------------------- sandboxes

// Sandbox traps mail: nothing sent to it is delivered.
type Sandbox struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
	// InboxAddress: mail sent to this address lands in the sandbox.
	InboxAddress  string     `json:"inboxAddress"`
	InboundLive   bool       `json:"inboundLive"`
	ProjectLabel  string     `json:"projectLabel"`
	Color         string     `json:"color"`
	Total         int        `json:"total"`
	Unread        int        `json:"unread"`
	MessageCap    int        `json:"messageCap"`
	RetentionDays int        `json:"retentionDays"`
	LastTrappedAt *time.Time `json:"lastTrappedAt"`
	CreatedAt     time.Time  `json:"createdAt"`
}

// SandboxParams creates or renames a sandbox.
type SandboxParams struct {
	Name         string `json:"name,omitempty"`
	ProjectLabel string `json:"projectLabel,omitempty"`
	Color        string `json:"color,omitempty"` // #rrggbb
}

// SandboxCredentials point a project at the sandbox.
type SandboxCredentials struct {
	Host            string `json:"host"`
	Port            int    `json:"port"`
	TLS             string `json:"tls"` // required, optional or none
	MaxRecipients   int    `json:"maxRecipients"`
	Username        string `json:"username"`
	Password        string `json:"password"`
	MaxMessageBytes int    `json:"maxMessageBytes"`
	RetentionDays   int    `json:"retentionDays"`
	APIEndpoint     string `json:"apiEndpoint"`
	InboxID         string `json:"inboxId"`
	InboxAddress    string `json:"inboxAddress"`
	InboundDomain   string `json:"inboundDomain"`
	InboundLive     bool   `json:"inboundLive"`
	// APITokenHint: the JSON token is hashed; only its hint can be read back.
	APITokenHint string `json:"apiTokenHint"`
}

// ResetSandboxCredentials holds the new credentials. The token is shown once.
type ResetSandboxCredentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
	APIToken string `json:"apiToken"`
}

// SandboxMessageSummary is one trapped message in a list.
type SandboxMessageSummary struct {
	ID          string    `json:"id"`
	Subject     string    `json:"subject"`
	FromAddress string    `json:"fromAddress"`
	FromName    string    `json:"fromName"`
	ToAddresses []string  `json:"toAddresses"`
	Snippet     string    `json:"snippet"`
	SizeBytes   int       `json:"sizeBytes"`
	Read        bool      `json:"read"`
	TrappedAt   time.Time `json:"trappedAt"`
}

// SandboxMessage is one trapped message in full.
type SandboxMessage struct {
	SandboxMessageSummary
	CcAddresses []string `json:"ccAddresses"`
	// EnvelopeFrom/EnvelopeTo are the SMTP envelope, recorded apart from the
	// headers (a Bcc recipient is only here).
	EnvelopeFrom string   `json:"envelopeFrom"`
	EnvelopeTo   []string `json:"envelopeTo"`
	BodyHTML     *string  `json:"bodyHtml"`
	// BodyText is nil when the message carried no text/plain part.
	BodyText *string `json:"bodyText"`
	Headers  []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"headers"`
	Transport string  `json:"transport"`
	ClientIP  *string `json:"clientIp"`
	HasRaw    bool    `json:"hasRaw"`
}

// ListSandboxMessagesParams filters a sandbox's messages.
type ListSandboxMessagesParams struct {
	Search string
	Limit  int
}

// SandboxSendResult is what the sandbox JSON door answers.
type SandboxSendResult struct {
	ID        string `json:"id"`
	Duplicate bool   `json:"duplicate,omitempty"`
}

// -------------------------------------------------------------- webhooks

// WebhookEventType names an event a webhook can subscribe to.
type WebhookEventType string

const (
	EventDelivered  WebhookEventType = "email.delivered"
	EventOpened     WebhookEventType = "email.opened"
	EventClicked    WebhookEventType = "email.clicked"
	EventBounced    WebhookEventType = "email.bounced"
	EventComplained WebhookEventType = "email.complained"
	EventFailed     WebhookEventType = "email.failed"
)

// WebhookEndpoint is a URL SendClipper posts events to.
type WebhookEndpoint struct {
	ID                      string             `json:"id"`
	URL                     string             `json:"url"`
	Events                  []WebhookEventType `json:"events"`
	Active                  bool               `json:"active"`
	CreatedAt               time.Time          `json:"createdAt"`
	SecretMasked            string             `json:"secretMasked"`
	SecretRotatedAt         *time.Time         `json:"secretRotatedAt"`
	PreviousSecretExpiresAt *time.Time         `json:"previousSecretExpiresAt"`
	SuccessRate             *float64           `json:"successRate"`
	SuccessRateWindowDays   int                `json:"successRateWindowDays"`
	DeliveryCount           int                `json:"deliveryCount"`
	DeliveredCount          int                `json:"deliveredCount"`
}

// WebhookEndpointWithSecret carries the full whsec_ signing secret, once.
type WebhookEndpointWithSecret struct {
	WebhookEndpoint
	Secret string `json:"secret"`
}

// CreateWebhookParams registers an endpoint.
type CreateWebhookParams struct {
	URL    string             `json:"url"`
	Events []WebhookEventType `json:"events"`
	// AllowInsecureHTTP accepts a plain http:// URL. Off by default.
	AllowInsecureHTTP bool `json:"allowInsecureHttp,omitempty"`
}

// UpdateWebhookParams changes the fields you set.
type UpdateWebhookParams struct {
	URL               string             `json:"url,omitempty"`
	Events            []WebhookEventType `json:"events,omitempty"`
	Active            *bool              `json:"active,omitempty"`
	AllowInsecureHTTP *bool              `json:"allowInsecureHttp,omitempty"`
}

// ListWebhookDeliveriesParams filters delivery attempts.
type ListWebhookDeliveriesParams struct {
	EndpointID string
	FinalOnly  *bool
	Limit      int
}

// ListWebhookDispatchesParams filters undelivered events.
type ListWebhookDispatchesParams struct {
	// State defaults to dead_lettered; "retrying" = pending or delivering; "all".
	State      string
	EndpointID string
	Cursor     string
	Limit      int // 1-100
}

// WebhookDispatch is one event's delivery to one endpoint.
type WebhookDispatch struct {
	ID          string `json:"id"`
	EndpointID  string `json:"endpointId"`
	EndpointURL string `json:"endpointUrl"`
	// EventID is the id the receiver saw in the body.
	EventID          string           `json:"eventId"`
	Event            WebhookEventType `json:"event"`
	State            string           `json:"state"` // pending, delivering, delivered or dead_lettered
	Attempts         int              `json:"attempts"`
	LastStatusCode   *int             `json:"lastStatusCode"`
	LastError        *string          `json:"lastError"`
	LastAttemptError *string          `json:"lastAttemptError"`
	FirstAttemptAt   *time.Time       `json:"firstAttemptAt"`
	LastAttemptAt    *time.Time       `json:"lastAttemptAt"`
	NextAttemptAt    *time.Time       `json:"nextAttemptAt"`
	CreatedAt        time.Time        `json:"createdAt"`
	ReplayedAt       *time.Time       `json:"replayedAt"`
	ReplayCount      int              `json:"replayCount"`
}

// WebhookDispatchPage is one cursor page of dispatches.
type WebhookDispatchPage struct {
	Data []WebhookDispatch `json:"data"`
	Page CursorInfo        `json:"page"`
}

// ReplayAllParams scopes a bulk replay.
type ReplayAllParams struct {
	State      string `json:"state,omitempty"`
	EndpointID string `json:"endpointId,omitempty"`
}

// ReplayAllResult says what a bulk replay queued.
type ReplayAllResult struct {
	Queued    int  `json:"queued"`
	Remaining int  `json:"remaining"`
	Capped    bool `json:"capped"`
	Max       int  `json:"max"`
}

// WebhookEvent is a webhook delivery's body.
type WebhookEvent struct {
	// ID is stable for one (email, event): deduplicate on it.
	ID        string           `json:"id"`
	Event     WebhookEventType `json:"event"`
	CreatedAt time.Time        `json:"createdAt"`
	Data      WebhookEventData `json:"data"`
}

// WebhookEventData is the message an event is about. Raw holds every field.
type WebhookEventData struct {
	EmailID   string `json:"emailId"`
	MessageID string `json:"messageId"`
	To        string `json:"to"`
	Domain    string `json:"domain"`
	Subject   string `json:"subject"`
	// Raw is the whole data object, for event-specific fields (bounce type, clicked link, ...).
	Raw map[string]any `json:"-"`
}

// UnmarshalJSON keeps every field in Raw as well as the typed ones.
func (d *WebhookEventData) UnmarshalJSON(b []byte) error {
	type plain WebhookEventData
	if err := json.Unmarshal(b, (*plain)(d)); err != nil {
		return err
	}
	return json.Unmarshal(b, &d.Raw)
}

// ------------------------------------------------- contacts & audiences

// Contact is one person in an audience.
type Contact struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Email         string    `json:"email"`
	Status        string    `json:"status"` // subscribed, unsubscribed or bounced
	AudienceID    *string   `json:"audienceId"`
	AudienceName  *string   `json:"audienceName"`
	AudienceColor *string   `json:"audienceColor"`
	CreatedAt     time.Time `json:"createdAt"`
}

// ContactList is one numbered page of contacts.
type ContactList struct {
	Items      []Contact `json:"items"`
	Total      int       `json:"total"`
	Page       int       `json:"page"`
	PageSize   int       `json:"pageSize"`
	TotalPages int       `json:"totalPages"`
	TotalAll   int       `json:"totalAll"`
}

// CreateContactParams adds a contact.
type CreateContactParams struct {
	Name       string `json:"name"`
	Email      string `json:"email"`
	AudienceID string `json:"audienceId,omitempty"`
	Status     string `json:"status,omitempty"`
}

// ListContactsParams filters contacts.
type ListContactsParams struct {
	AudienceID string
	Status     string
	Page       int
	PageSize   int // 1-200, default 25
}

// ImportContactsParams is a CSV or vCard import.
type ImportContactsParams struct {
	// File is the CSV or vCard contents.
	File []byte
	// Filename (contacts.csv / contacts.vcf): the extension decides the parser.
	Filename        string
	AudienceID      string
	NewAudience     string
	CreateAudiences *bool
	// OnDuplicate is "skip" or "update".
	OnDuplicate string
	// Mapping is the column mapping as ImportPreview returns it.
	Mapping map[string]any
}

// ExportContactsParams picks the export format.
type ExportContactsParams struct {
	Format   string // csv (default) or vcf
	Audience string
}

// Audience is a list of contacts.
type Audience struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
	Count int    `json:"count"`
}

// AudienceList is every audience.
type AudienceList struct {
	Audiences     []Audience `json:"audiences"`
	TotalContacts int        `json:"totalContacts"`
}

// ---------------------------------------------------------- suppressions

// Suppression stops mail to one address.
type Suppression struct {
	ID              string    `json:"id"`
	Email           string    `json:"email"`
	Reason          string    `json:"reason"` // hard_bounce, soft_bounce, complaint, unsubscribe, manual, provider
	ReasonLabel     string    `json:"reasonLabel"`
	ReasonDetail    *string   `json:"reasonDetail"`
	DiagnosticCode  *string   `json:"diagnosticCode"`
	Scope           string    `json:"scope"` // marketing for an unsubscribe, all for everything else
	SourceEmailID   *string   `json:"sourceEmailId"`
	SourceMessageID *string   `json:"sourceMessageId"`
	CreatedAt       time.Time `json:"createdAt"`
}

// SuppressionList is one cursor page of the list.
type SuppressionList struct {
	Data   []Suppression `json:"data"`
	Page   CursorInfo    `json:"page"`
	Counts struct {
		Total    int            `json:"total"`
		ByReason map[string]int `json:"byReason"`
	} `json:"counts"`
}

// ListSuppressionsParams filters the list.
type ListSuppressionsParams struct {
	Q      string // address search
	Reason string
	Cursor string
	Limit  int // 1-100
}

// CreateSuppressionParams suppresses an address (reason manual).
type CreateSuppressionParams struct {
	Email string `json:"email"`
	Note  string `json:"note,omitempty"`
}

// SuppressionCreated: Created is false when the address was already
// suppressed; Row is the entry either way.
type SuppressionCreated struct {
	Created bool         `json:"created"`
	Row     *Suppression `json:"row"`
}

// ------------------------------------------------------------- mailboxes

// Mailbox is one address on a receiving domain.
type Mailbox struct {
	ID          string `json:"id"`
	Address     string `json:"address"`
	DisplayName string `json:"displayName"`
	Domain      string `json:"domain"`
	DomainID    string `json:"domainId"`
	CatchAll    bool   `json:"catchAll"`
	Archived    bool   `json:"archived"`
	CanReceive  bool   `json:"canReceive"`
	CanSend     bool   `json:"canSend"`
	Unread      int    `json:"unread"`
	Total       int    `json:"total"`
}

// CreateMailboxParams makes an address.
type CreateMailboxParams struct {
	DomainID    string `json:"domainId"`
	LocalPart   string `json:"localPart"`
	DisplayName string `json:"displayName,omitempty"`
	Color       string `json:"color,omitempty"`
	CatchAll    bool   `json:"catchAll,omitempty"`
}

// UpdateMailboxParams changes the fields you set.
type UpdateMailboxParams struct {
	LocalPart    string   `json:"localPart,omitempty"`
	DisplayName  *string  `json:"displayName,omitempty"`
	Color        string   `json:"color,omitempty"`
	CatchAll     *bool    `json:"catchAll,omitempty"`
	Archived     *bool    `json:"archived,omitempty"`
	NotifyOwner  *bool    `json:"notifyOwner,omitempty"`
	EmbedOrigins []string `json:"embedOrigins,omitempty"`
}

// ListMailboxMessagesParams filters an address's mail.
type ListMailboxMessagesParams struct {
	Folder string // all, inbox, starred, sent, drafts, archive, spam, trash
	Search string
	Label  string
	Limit  int // 1-200
	Cursor string
}

// MailboxMessageRow is one message in a mailbox list.
type MailboxMessageRow struct {
	ID         string    `json:"id"`
	Folder     string    `json:"folder"`
	From       string    `json:"from"`
	FromName   string    `json:"fromName"`
	To         string    `json:"to"`
	Cc         *string   `json:"cc"`
	Bcc        *string   `json:"bcc"`
	Subject    string    `json:"subject"`
	Snippet    string    `json:"snippet"`
	Label      *string   `json:"label"`
	Unread     bool      `json:"unread"`
	Starred    bool      `json:"starred"`
	ReceivedAt time.Time `json:"receivedAt"`
	MailboxID  *string   `json:"mailboxId"`
	ThreadID   *string   `json:"threadId"`
}

// MailboxMessageList is one cursor page of a mailbox.
type MailboxMessageList struct {
	Items       []MailboxMessageRow `json:"items"`
	Total       int                 `json:"total"`
	Unread      int                 `json:"unread"`
	TotalCapped bool                `json:"totalCapped"`
	Page        CursorInfo          `json:"page"`
}

// MailboxMessage is one message in full, with its conversation.
type MailboxMessage struct {
	MailboxMessageRow
	BodyText      *string    `json:"bodyText"`
	BodyHTML      *string    `json:"bodyHtml"`
	BodyExpiredAt *time.Time `json:"bodyExpiredAt"`
	Thread        *struct {
		ID           string          `json:"id"`
		Subject      string          `json:"subject"`
		MessageCount int             `json:"messageCount"`
		LastAt       time.Time       `json:"lastAt"`
		Participants []string        `json:"participants"`
		Messages     json.RawMessage `json:"messages"`
	} `json:"thread"`
	// Attachments: download one with Mailboxes.Messages.Attachment.
	Attachments []struct {
		ID          string `json:"id"`
		Filename    string `json:"filename"`
		ContentType string `json:"contentType"`
		SizeBytes   int    `json:"sizeBytes"`
	} `json:"attachments"`
	// Authentication holds SPF/DKIM/DMARC/spam verdicts for received mail; nil for your own sends.
	Authentication map[string]any `json:"authentication"`
}

// DownloadedFile is what a download returns.
type DownloadedFile struct {
	Content     []byte
	ContentType string
	Filename    string
}

// ------------------------------------------------------------- workspace

// Workspace is the workspace an API key belongs to.
type Workspace struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	PublicID      string    `json:"publicId"`
	CreatedAt     time.Time `json:"createdAt"`
	SendingPaused *struct {
		At      time.Time `json:"at"`
		Reason  string    `json:"reason"`
		Message string    `json:"message"`
	} `json:"sendingPaused"`
	PostalAddress *string `json:"postalAddress"`
}

// APIKey is one of the workspace's keys, masked.
type APIKey struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Masked     string     `json:"masked"`
	Prefix     string     `json:"prefix"`
	Last4      string     `json:"last4"`
	Permission string     `json:"permission"` // full, sending or read
	DomainID   *string    `json:"domainId"`
	Domain     *string    `json:"domain"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
}
