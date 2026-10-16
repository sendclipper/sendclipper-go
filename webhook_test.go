package sendclipper

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

const (
	whSecret = "whsec_test_secret"
	whT      = int64(1756800000)
	whBody   = `{"id":"evt_1","event":"email.delivered","createdAt":"2026-09-06T09:41:02.518Z","data":{"emailId":"e1","messageId":"m1","to":"user@acme.io","domain":"send.acme.io","subject":"Your receipt"}}`
	// Computed with `openssl dgst -sha256 -hmac`, not with this SDK (the Node SDK's vector).
	whSig    = "76921f5ba77ae7f38686e8f147da3903c0ff87100cbd47ee466b7d646d198192"
	whHeader = "t=1756800000,v1=" + whSig
)

var at = time.Unix(whT, 0)

func refused(t *testing.T, reason string, err error) {
	t.Helper()
	var e *WebhookVerificationError
	if !errors.As(err, &e) || e.Reason != reason {
		t.Fatalf("want %s, got %v", reason, err)
	}
}

func TestAGenuineDeliveryVerifiesAndReturnsTheParsedEvent(t *testing.T) {
	h := http.Header{}
	h.Set("X-SendClipper-Signature-V2", whHeader)
	ev, err := VerifyWebhook([]byte(whBody), h, whSecret, WithNow(at.Add(10*time.Second)))
	if err != nil {
		t.Fatal(err)
	}
	if ev.ID != "evt_1" || ev.Event != EventDelivered || ev.Data.To != "user@acme.io" || ev.Data.Raw["subject"] != "Your receipt" || ev.CreatedAt.Year() != 2026 {
		t.Fatalf("%+v", ev)
	}
}

func TestTheHeaderIsFoundWhateverItsCase(t *testing.T) {
	h := http.Header{"X-Sendclipper-Signature-V2": {whHeader}} // canonical form, as net/http stores it
	if _, err := VerifyWebhook([]byte(whBody), h, whSecret, WithNow(at)); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyWebhookSignature([]byte(whBody), whHeader, whSecret, WithNow(at)); err != nil {
		t.Fatal(err)
	}
}

func TestDuringARotationEitherSignatureIsAccepted(t *testing.T) {
	other := strings.Repeat("a", 64)
	for _, sig := range []string{"t=1756800000,v1=" + other + ",v1=" + whSig, whHeader + ",v1=" + other} {
		if _, err := VerifyWebhookSignature([]byte(whBody), sig, whSecret, WithNow(at)); err != nil {
			t.Fatalf("%s: %v", sig, err)
		}
	}
	// Two header lines are joined, not one picked.
	h := http.Header{}
	h.Add(SignatureHeader, "t=1756800000,v1="+other)
	h.Add(SignatureHeader, "v1="+whSig)
	if _, err := VerifyWebhook([]byte(whBody), h, whSecret, WithNow(at)); err != nil {
		t.Fatal(err)
	}
}

func TestAChangedBodyAWrongSecretOrAWrongSignatureIsRefused(t *testing.T) {
	_, err := VerifyWebhookSignature([]byte(strings.Replace(whBody, "user@", "evil@", 1)), whHeader, whSecret, WithNow(at))
	refused(t, "signature_mismatch", err)
	_, err = VerifyWebhookSignature([]byte(whBody), whHeader, "whsec_other", WithNow(at))
	refused(t, "signature_mismatch", err)
	_, err = VerifyWebhookSignature([]byte(whBody), "t=1756800000,v1="+strings.Repeat("0", 64), whSecret, WithNow(at))
	refused(t, "signature_mismatch", err)
}

func TestReEncodedJSONFailsBecauseTheSignatureCoversTheRawBytes(t *testing.T) {
	var buf bytes.Buffer
	_ = json.Indent(&buf, []byte(whBody), "", "  ")
	_, err := VerifyWebhookSignature(buf.Bytes(), whHeader, whSecret, WithNow(at))
	refused(t, "signature_mismatch", err)
}

func TestATimestampOutsideTheToleranceIsRefused(t *testing.T) {
	_, err := VerifyWebhookSignature([]byte(whBody), whHeader, whSecret, WithNow(at.Add(301*time.Second)))
	refused(t, "timestamp_out_of_tolerance", err)
	_, err = VerifyWebhookSignature([]byte(whBody), whHeader, whSecret, WithNow(at.Add(-301*time.Second)))
	refused(t, "timestamp_out_of_tolerance", err)
	if _, err := VerifyWebhookSignature([]byte(whBody), whHeader, whSecret, WithNow(at.Add(time.Hour)), WithTolerance(time.Hour)); err != nil {
		t.Fatal(err)
	}
}

func TestAMissingOrMalformedHeaderIsRefused(t *testing.T) {
	_, err := VerifyWebhook([]byte(whBody), http.Header{}, whSecret, WithNow(at))
	refused(t, "missing_header", err)
	_, err = VerifyWebhookSignature([]byte(whBody), whHeader, "", WithNow(at))
	refused(t, "missing_header", err)
	for _, h := range []string{"v1=" + whSig, "t=1756800000", "t=abc,v1=" + whSig, "t=-5,v1=" + whSig} {
		_, err = VerifyWebhookSignature([]byte(whBody), h, whSecret, WithNow(at))
		refused(t, "malformed_header", err)
	}
}

func TestASignedBodyThatIsNotAnEventIsRefused(t *testing.T) {
	body := []byte("not json")
	_, err := VerifyWebhookSignature(body, WebhookSignatureHeader(whSecret, body, whT), whSecret, WithNow(at))
	refused(t, "invalid_payload", err)
}

func TestWebhookSignatureHeaderBuildsWhatVerifyAccepts(t *testing.T) {
	h := WebhookSignatureHeader(whSecret, []byte(whBody), whT)
	if h != whHeader {
		t.Fatalf("%s", h)
	}
	ts, sigs, ok := ParseSignatureHeader(h)
	if !ok || ts != whT || len(sigs) != 1 || sigs[0] != whSig {
		t.Fatal(ts, sigs, ok)
	}
}

func TestTheClientsWebhooksVerifyIsTheSameCheck(t *testing.T) {
	c := NewClient("k", WithBaseURL("http://127.0.0.1:9"))
	h := http.Header{}
	h.Set(SignatureHeader, whHeader)
	if ev, err := c.Webhooks.Verify([]byte(whBody), h, whSecret, WithNow(at)); err != nil || ev.ID != "evt_1" {
		t.Fatal(err)
	}
}
