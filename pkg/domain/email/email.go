package email

type EmailMessage struct {
	FromEmail     string
	FromName      string
	ToEmail       string
	Subject       string
	HTMLBody      string
	PlainTextBody string
}
