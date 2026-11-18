package email

import "strings"

// EmailMessage is what every Kiban tool hands to an email service.
//
// The shape follows the SendgridEmailRequest that kiban cloud sends to
// link (see go-kiban, service/link/model): several recipients, CC, BCC
// and attachments. Consumers that only ever send one plain message keep
// using ToEmail and ignore the rest — every field added here is
// optional, so nothing that compiled before stops compiling.
//
// Before this, each consumer worked around the gap by dropping to the
// raw SendGrid client: datos-non-stop and consulta-por-kiban both did it
// for BCC, and would have done it again for attachments.
type EmailMessage struct {
	FromEmail string
	FromName  string

	// ToEmail is the single-recipient form, and ToEmails the list one.
	// Both are honoured and merged, so a caller can use either; a
	// duplicate address is only sent to once.
	ToEmail  string
	ToEmails []string

	CC  []string
	BCC []string

	Subject       string
	HTMLBody      string
	PlainTextBody string

	Attachments []Attachment
}

// Attachment is a file sent along with the message. Content is the raw
// bytes: base64 is an encoding detail of the transport, so the service
// does it, not the caller.
type Attachment struct {
	Filename    string
	ContentType string
	Content     []byte
}

// Destinatarios returns the recipients of the message: ToEmail and
// ToEmails merged, trimmed, de-duplicated case-insensitively and in the
// order they were given.
func (m EmailMessage) Destinatarios() []string {
	return unique(append([]string{m.ToEmail}, m.ToEmails...), nil)
}

// CopiasOcultas returns the BCC list without anyone already receiving
// the message as a recipient — SendGrid would otherwise deliver twice to
// the same address, which looks like a bug to whoever receives it.
func (m EmailMessage) CopiasOcultas() []string {
	return unique(m.BCC, m.Destinatarios())
}

// Copias returns the CC list, minus the recipients, on the same grounds.
func (m EmailMessage) Copias() []string {
	return unique(m.CC, m.Destinatarios())
}

func unique(values, exclude []string) []string {
	seen := make(map[string]struct{}, len(values)+len(exclude))
	for _, e := range exclude {
		seen[normalize(e)] = struct{}{}
	}

	out := make([]string, 0, len(values))
	for _, v := range values {
		key := normalize(v)
		if key == "" {
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, trim(v))
	}
	return out
}

func normalize(v string) string { return strings.ToLower(trim(v)) }

func trim(v string) string { return strings.TrimSpace(v) }
