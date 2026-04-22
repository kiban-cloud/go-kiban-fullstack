package email

import (
	"context"
	"fmt"

	email_domain "github.com/kiban-cloud/go-kiban-fullstack/pkg/domain/email"

	errorsWrapper "github.com/pkg/errors"
	"github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"
)

type SendGridService struct {
	apiKey string
	client *sendgrid.Client
}

func NewSendGridService(apiKey string) *SendGridService {
	return &SendGridService{
		apiKey: apiKey,
		client: sendgrid.NewSendClient(apiKey),
	}
}

func (s *SendGridService) Send(ctx context.Context, emailMessage email_domain.EmailMessage) error {
	from := mail.NewEmail(emailMessage.FromName, emailMessage.FromEmail)
	toEmail := mail.NewEmail("", emailMessage.ToEmail)
	message := mail.NewSingleEmail(from, emailMessage.Subject, toEmail, emailMessage.PlainTextBody, emailMessage.HTMLBody)

	response, err := s.client.Send(message)
	if err != nil {
		return errorsWrapper.Wrap(err, "error when trying to send email")
	}

	if response.StatusCode >= 400 {
		return errorsWrapper.Wrap(fmt.Errorf("sendgrid error: status %d", response.StatusCode), "error when trying to send email")
	}

	return nil
}
