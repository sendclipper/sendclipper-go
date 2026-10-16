package sendclipper

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Error is what every failed call returns: an API refusal, or a request
// that never got an answer. Branch on Name, SendClipper's machine-readable
// code; Message is a sentence meant for a person.
type Error struct {
	// Name is the API's code: "domain_not_verified", "template_variables_missing",
	// "validation_failed", "rate_limited", or "network_error" when no answer arrived.
	Name string
	// Message is a sentence meant for a person.
	Message string
	// StatusCode is the HTTP status, or 0 when no answer arrived.
	StatusCode int
	// Detail holds code-specific facts (e.g. the missing template tags), as JSON.
	Detail json.RawMessage
	// Headers are the response headers, or nil when no answer arrived.
	Headers http.Header

	cause error
}

func (e *Error) Error() string {
	if e.StatusCode == 0 {
		return fmt.Sprintf("sendclipper: %s: %s", e.Name, e.Message)
	}
	return fmt.Sprintf("sendclipper: %s (%d): %s", e.Name, e.StatusCode, e.Message)
}

// Unwrap returns the transport error behind a "network_error".
func (e *Error) Unwrap() error { return e.cause }

// toAPIError reads the two shapes the API answers refusals in: the house
// envelope { error: { code, message, detail? } } and Nest's default
// { statusCode, message: string | string[], error } (request validation).
func toAPIError(status int, body []byte, headers http.Header) *Error {
	e := &Error{StatusCode: status, Headers: headers}
	var obj map[string]json.RawMessage
	_ = json.Unmarshal(body, &obj)

	type envelope struct {
		Code    any             `json:"code"`
		Message any             `json:"message"`
		Detail  json.RawMessage `json:"detail"`
		Details json.RawMessage `json:"details"`
	}
	fromEnvelope := func(raw json.RawMessage) bool {
		var env envelope
		if len(raw) == 0 || raw[0] != '{' || json.Unmarshal(raw, &env) != nil {
			return false
		}
		e.Name = nonEmpty(scalar(env.Code), codeForStatus(status))
		e.Message = nonEmpty(scalar(env.Message), fmt.Sprintf("HTTP %d", status))
		e.Detail = env.Detail
		if len(e.Detail) == 0 {
			e.Detail = env.Details
		}
		return true
	}
	if fromEnvelope(obj["error"]) {
		return e
	}
	if msg := obj["message"]; len(msg) > 0 {
		var inner map[string]json.RawMessage
		if msg[0] == '{' && json.Unmarshal(msg, &inner) == nil && fromEnvelope(inner["error"]) {
			return e
		}
		var list []any
		if msg[0] == '[' && json.Unmarshal(msg, &list) == nil {
			parts := make([]string, len(list))
			for i, m := range list {
				parts[i] = fmt.Sprint(m)
			}
			e.Name, e.Message, e.Detail = "validation_failed", strings.Join(parts, "; "), msg
			return e
		}
		var s string
		if json.Unmarshal(msg, &s) == nil {
			e.Name, e.Message = codeForStatus(status), s
			return e
		}
	}
	snippet := strings.TrimSpace(string(body))
	if len(snippet) > 300 {
		snippet = snippet[:300]
	}
	e.Name, e.Message = codeForStatus(status), nonEmpty(snippet, fmt.Sprintf("HTTP %d", status))
	return e
}

func scalar(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}

func nonEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func codeForStatus(status int) string {
	switch status {
	case 400:
		return "bad_request"
	case 401:
		return "unauthorized"
	case 403:
		return "forbidden"
	case 404:
		return "not_found"
	case 409:
		return "conflict"
	case 413:
		return "payload_too_large"
	case 429:
		return "rate_limited"
	default:
		return fmt.Sprintf("http_%d", status)
	}
}
