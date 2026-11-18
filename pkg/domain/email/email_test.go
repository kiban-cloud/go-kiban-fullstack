package email_test

import (
	"testing"

	"github.com/kiban-cloud/go-kiban-fullstack/pkg/domain/email"

	"github.com/stretchr/testify/assert"
)

func TestEmailMessage_Destinatarios(t *testing.T) {
	t.Run("merges the single and the list form", func(t *testing.T) {
		m := email.EmailMessage{
			ToEmail:  "uno@kiban.com",
			ToEmails: []string{"dos@kiban.com"},
		}
		assert.Equal(t, []string{"uno@kiban.com", "dos@kiban.com"}, m.Destinatarios())
	})

	t.Run("drops duplicates regardless of case and spacing", func(t *testing.T) {
		m := email.EmailMessage{
			ToEmail:  "uno@kiban.com",
			ToEmails: []string{" UNO@kiban.com ", "dos@kiban.com"},
		}
		assert.Equal(t, []string{"uno@kiban.com", "dos@kiban.com"}, m.Destinatarios())
	})

	t.Run("ignores empty entries", func(t *testing.T) {
		m := email.EmailMessage{ToEmails: []string{"", "   ", "uno@kiban.com"}}
		assert.Equal(t, []string{"uno@kiban.com"}, m.Destinatarios())
	})

	t.Run("is empty when nothing was set", func(t *testing.T) {
		assert.Empty(t, email.EmailMessage{}.Destinatarios())
	})
}

func TestEmailMessage_CopiasOcultas(t *testing.T) {
	// Sending to an address both as recipient and as BCC delivers it
	// twice, which reads as a bug to whoever gets it.
	t.Run("excludes anyone already a recipient", func(t *testing.T) {
		m := email.EmailMessage{
			ToEmail: "cliente@example.com",
			BCC:     []string{"CLIENTE@example.com", "equipo@kiban.com"},
		}
		assert.Equal(t, []string{"equipo@kiban.com"}, m.CopiasOcultas())
	})

	t.Run("drops duplicates inside the list", func(t *testing.T) {
		m := email.EmailMessage{
			ToEmail: "cliente@example.com",
			BCC:     []string{"equipo@kiban.com", "equipo@kiban.com"},
		}
		assert.Equal(t, []string{"equipo@kiban.com"}, m.CopiasOcultas())
	})
}

func TestEmailMessage_Copias(t *testing.T) {
	t.Run("excludes anyone already a recipient", func(t *testing.T) {
		m := email.EmailMessage{
			ToEmails: []string{"cliente@example.com"},
			CC:       []string{"cliente@example.com", "contador@example.com"},
		}
		assert.Equal(t, []string{"contador@example.com"}, m.Copias())
	})
}
