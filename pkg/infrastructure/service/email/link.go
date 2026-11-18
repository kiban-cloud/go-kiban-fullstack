package email

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	email_domain "github.com/kiban-cloud/go-kiban-fullstack/pkg/domain/email"

	errorsWrapper "github.com/pkg/errors"
)

// LinkEmailService sends mail through Link's sendgrid/email endpoint,
// which is how every Kiban tool is meant to send it: the SendGrid
// account, its sender identities and its reputation are managed in one
// place instead of one per service.
//
// It always uses htmlContent + subject, never templateId. The super
// template mode — templateId plus templateVariables, which wrapped the
// message in the kiban cloud layout — is deprecated as of 2026-11-18 and
// must not be used in new code: each product sends its own brand, and
// Link forwards the HTML as the message body, unchanged.
//
// Reference: the contract in go-kiban (service/link/model,
// SendgridEmailRequest) and the standalone port in facturapiscript
// (kiban/mailer).
type LinkEmailService struct {
	linkURL         string
	linkInternalKey string
	sendgridAPIKey  string
	client          *http.Client
}

const (
	rutaEmail        = "%s/api/v1/sendgrid/email"
	headerLinkAPIKey = "x-api-key"
	timeoutEnvio     = 60 * time.Second
)

// NewLinkEmailService builds the service. sendgridAPIKey is the key of
// the SendGrid account the mail goes out from — Link relays with the
// caller's account, so each product keeps its own verified senders.
func NewLinkEmailService(linkURL, linkInternalKey, sendgridAPIKey string) *LinkEmailService {
	return &LinkEmailService{
		linkURL:         strings.TrimRight(linkURL, "/"),
		linkInternalKey: linkInternalKey,
		sendgridAPIKey:  sendgridAPIKey,
		client:          &http.Client{Timeout: timeoutEnvio},
	}
}

// peticionEmail is the body Link expects.
type peticionEmail struct {
	ApiKey      string            `json:"apiKey"`
	From        string            `json:"from"`
	SenderName  string            `json:"senderName"`
	To          []string          `json:"to"`
	CC          []string          `json:"cc,omitempty"`
	BCC         []string          `json:"bcc,omitempty"`
	Subject     string            `json:"subject"`
	HtmlContent string            `json:"htmlContent"`
	Attachments map[string]string `json:"attachments,omitempty"`
}

func (s *LinkEmailService) Send(ctx context.Context, m email_domain.EmailMessage) error {
	destinatarios := m.Destinatarios()
	if len(destinatarios) == 0 {
		return errorsWrapper.New("the email has no recipients")
	}
	if m.HTMLBody == "" {
		// Link needs htmlContent or templateId; without either it answers
		// INPUT_ERROR on templateId, which points nowhere near the cause.
		return errorsWrapper.New("the email has no HTML body")
	}
	if s.sendgridAPIKey == "" || s.linkInternalKey == "" {
		// Link would fail with an error that does not say which key is
		// missing. Better to say it here.
		return errorsWrapper.New("missing the SendGrid key or Link's internal key")
	}

	adjuntos := make(map[string]string, len(m.Attachments))
	for _, a := range m.Attachments {
		if len(a.Content) == 0 {
			return errorsWrapper.New(fmt.Sprintf("the attachment %q is empty", a.Filename))
		}
		adjuntos[a.Filename] = base64.StdEncoding.EncodeToString(a.Content)
	}

	cuerpo, err := json.Marshal(peticionEmail{
		ApiKey:      s.sendgridAPIKey,
		From:        m.FromEmail,
		SenderName:  m.FromName,
		To:          destinatarios,
		CC:          m.Copias(),
		BCC:         m.CopiasOcultas(),
		Subject:     m.Subject,
		HtmlContent: m.HTMLBody,
		Attachments: adjuntos,
	})
	if err != nil {
		return errorsWrapper.Wrap(err, "error encoding the email request")
	}

	url := fmt.Sprintf(rutaEmail, s.linkURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(cuerpo))
	if err != nil {
		return errorsWrapper.Wrap(err, "error building the email request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("accept", "application/json")
	req.Header.Set(headerLinkAPIKey, s.linkInternalKey)

	resp, err := s.client.Do(req)
	if err != nil {
		return errorsWrapper.Wrap(err, "error when trying to send email through Link")
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		detalle, _ := io.ReadAll(resp.Body)
		return errorsWrapper.New(fmt.Sprintf("Link answered %d when sending the email: %s",
			resp.StatusCode, recortar(detalle)))
	}
	return nil
}

func recortar(datos []byte) string {
	s := strings.TrimSpace(string(datos))
	const max = 500
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
