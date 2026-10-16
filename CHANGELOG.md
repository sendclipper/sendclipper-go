# Changelog

## 0.1.0 — 2026-10-16

First release.

- `Emails.Send`, `Batch.Send`, `Emails.Get`, `Emails.List`, `Emails.All`, `Emails.Stats`.
- Templates (with tags and image assets), domains, sandboxes (including the JSON door and `WaitFor`), webhooks and their dispatches, contacts, audiences, suppressions, mailboxes, automations, workspace and API keys.
- Typed params and responses; every method takes a `context.Context`; every failure is a `*sendclipper.Error` whose `Name` is the API's code.
- Automatic `Idempotency-Key` on every JSON write; retries on network errors, 429 and 5xx.
- `VerifyWebhook()` for `X-SendClipper-Signature-V2`, rotation-aware.
- Go 1.23 or newer (range-over-func iterators); no dependencies outside the standard library.
