// Package emaildomain decides whether an email address belongs to a
// throwaway-mail provider, so signup can refuse it.
//
// It answers from two layers. The embedded list is a public aggregate of
// ~75k known disposable domains, compiled into the binary so the check
// costs nothing at runtime and cannot fail. On top of it sits a
// per-deployment override layer — in practice a Mongo collection — that
// exists because neither half works alone:
//
//   - The public list is not enough. The two domains that motivated this
//     (novelv.com, kolsea.com, September 2026) are absent from the small
//     curated lists; only the large aggregate has them, and the next one
//     that burns us will be absent from that too. Overrides are how a
//     domain gets blocked the same day it shows up.
//   - The public list is also not always right. It aggregates from many
//     sources, some aggressive, so a legitimate customer's domain can
//     land in it. Without a way to un-block, that customer has no way in
//     — which is why Allow beats everything else.
package emaildomain

import (
	"bufio"
	"context"
	"embed"
	"strings"
	"sync"
)

//go:embed disposable_domains.txt
var embeddedFS embed.FS

var (
	embeddedOnce sync.Once
	embedded     map[string]struct{}
)

// Overrides is the deployment-specific layer read at check time.
// Implementations are expected to be cheap to call.
type Overrides interface {
	// Load returns the domains this deployment blocks on top of the
	// embedded list, and the ones it allows despite it.
	Load(ctx context.Context) (blocked, allowed []string, err error)
}

// Checker reports whether an address is disposable.
type Checker struct {
	overrides Overrides
	// onOverridesError is called when the override layer cannot be read,
	// so the caller can log it. The check still answers from the embedded
	// list: refusing every signup because the override store is
	// unreachable is worse than letting one throwaway address through.
	onOverridesError func(error)
}

func NewChecker(overrides Overrides, onOverridesError func(error)) *Checker {
	return &Checker{overrides: overrides, onOverridesError: onOverridesError}
}

// IsDisposable reports whether the address belongs to a throwaway
// provider. An address it cannot parse is not disposable — rejecting
// malformed input is the caller's job, and answering "disposable" here
// would blame the wrong thing in the error the user sees.
func (c *Checker) IsDisposable(ctx context.Context, email string) bool {
	domain := DomainOf(email)
	if domain == "" {
		return false
	}

	var blocked, allowed []string
	if c.overrides != nil {
		var err error
		blocked, allowed, err = c.overrides.Load(ctx)
		if err != nil && c.onOverridesError != nil {
			c.onOverridesError(err)
		}
	}

	// An explicit allow wins over both the embedded list and an explicit
	// block: it is the escape hatch for a false positive, and it has to
	// work even if someone also listed the domain as blocked.
	if containsDomain(allowed, domain) {
		return false
	}
	if containsDomain(blocked, domain) {
		return true
	}

	_, found := embeddedList()[domain]
	return found
}

// DomainOf returns the lowercased domain of an address, or "" when there
// is nothing usable after the @.
func DomainOf(email string) string {
	at := strings.LastIndexByte(email, '@')
	if at < 0 || at == len(email)-1 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(email[at+1:]))
}

func containsDomain(domains []string, domain string) bool {
	for _, d := range domains {
		if strings.EqualFold(strings.TrimSpace(d), domain) {
			return true
		}
	}
	return false
}

// EmbeddedCount is how many domains the compiled-in list holds. Exposed
// so a deployment can log it at boot and notice a truncated embed.
func EmbeddedCount() int { return len(embeddedList()) }

func embeddedList() map[string]struct{} {
	embeddedOnce.Do(func() {
		f, err := embeddedFS.Open("disposable_domains.txt")
		if err != nil {
			// Cannot happen: the file is compiled into the binary.
			embedded = map[string]struct{}{}
			return
		}
		defer f.Close()

		embedded = make(map[string]struct{}, 80000)
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.ToLower(strings.TrimSpace(scanner.Text()))
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			embedded[line] = struct{}{}
		}
	})
	return embedded
}
