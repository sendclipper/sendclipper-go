# Changelog

## 0.2.0 — 2026-10-18

- `SendEmailParams.ReplyTo` (`[]string`, 1–10 bare addresses) sets the Reply-To header, on `Emails.Send`, `Sandboxes.Send` and each `Batch.Send` item.
- `SendEmailParams.Attachments` (`[]Attachment{Filename, Content, ContentType}`) sends files with the message. `Content` is the file's raw bytes; it is base64-encoded for you. `ContentType` is optional. Limits: 10 MiB per file, 25 MiB of files per message, 20 files. Not accepted on a batch item (`attachments_not_batchable`).
- Needs the SendClipper API from 2026-10-18 on; an older API ignores both fields.

## 0.1.1 — 2026-10-17

- README: how to install at a `go.work` root (run `go get` inside the module). No code change.

## 0.1.0 — 2026-10-16

First release.

- `Emails.Send`, `Batch.Send`, `Emails.Get`, `Emails.List`, `Emails.All`, `Emails.Stats`.
- Templates (with tags and image assets), domains, sandboxes (including the JSON door and `WaitFor`), webhooks and their dispatches, contacts, audiences, suppressions, mailboxes, automations, workspace and API keys.
- Typed params and responses; every method takes a `context.Context`; every failure is a `*sendclipper.Error` whose `Name` is the API's code.
- Automatic `Idempotency-Key` on every JSON write; retries on network errors, 429 and 5xx.
- `VerifyWebhook()` for `X-SendClipper-Signature-V2`, rotation-aware.
- Go 1.23 or newer (range-over-func iterators); no dependencies outside the standard library.
