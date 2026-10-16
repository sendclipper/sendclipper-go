package sendclipper_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/sendclipper/sendclipper-go"
)

func Example() {
	client := sendclipper.NewClient("sc_live_xxxxxxxxxxxxxxxx")

	email, err := client.Emails.Send(context.Background(), &sendclipper.SendEmailParams{
		From:    "hello@send.acme.io",
		To:      "ada@lumen.io",
		Subject: "Your receipt",
		HTML:    "<p>Thanks for your order.</p>",
		Text:    "Thanks for your order.",
	})
	var apiErr *sendclipper.Error
	if errors.As(err, &apiErr) {
		log.Println(apiErr.Name, apiErr.Message) // e.g. domain_not_verified
		return
	}
	fmt.Println("Email", email.ID, "accepted")
}

func ExampleEmailsService_Send_template() {
	client := sendclipper.NewClient("")
	_, err := client.Emails.Send(context.Background(), &sendclipper.SendEmailParams{
		From:       "hello@send.acme.io",
		To:         "ada@lumen.io",
		TemplateID: "tpl_9f2b7c10ad42", // or Template: "welcome"
		Variables:  map[string]any{"first_name": "Ada"},
	}, sendclipper.WithIdempotencyKey("order-42-receipt"))
	if err != nil {
		log.Println(err)
	}
}

func ExampleEmailsService_All() {
	client := sendclipper.NewClient("")
	for email, err := range client.Emails.All(context.Background(), &sendclipper.ListEmailsParams{Status: sendclipper.StatusBounced}) {
		if err != nil {
			log.Println(err)
			break
		}
		fmt.Println(email.To)
	}
}

func ExampleVerifyWebhook() {
	http.HandleFunc("/hooks/sendclipper", func(w http.ResponseWriter, r *http.Request) {
		payload, err := io.ReadAll(r.Body) // the raw body, before any json.Decode
		if err != nil {
			http.Error(w, "read error", http.StatusBadRequest)
			return
		}
		event, err := sendclipper.VerifyWebhook(payload, r.Header, os.Getenv("SENDCLIPPER_WEBHOOK_SECRET"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		// event.ID is stable across retries: deduplicate on it
		log.Println(event.Event, event.Data.To)
		w.WriteHeader(http.StatusOK)
	})
}
