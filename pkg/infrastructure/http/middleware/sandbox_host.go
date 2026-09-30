package middleware

import (
	"net"
	"strings"
)

// sandboxLabel is the DNS label that marks a host as the sandbox surface.
const sandboxLabel = "sandbox"

// IsSandboxHost reports whether host (a Request.Host, port optional) is a
// sandbox host: one of its DNS labels is exactly "sandbox". The label can sit
// anywhere, so prod-style names (sandbox.api.example.com) and the develop
// naming (develop.sandbox.api.example.kibantest.com) both match, as do the
// local/test hosts (sandbox.localhost:8080, sandbox.test). A label that merely
// contains the word (sandboxes.example.com, mysandbox.example.com) does not.
func IsSandboxHost(host string) bool {
	for _, l := range strings.Split(hostname(host), ".") {
		if strings.EqualFold(l, sandboxLabel) {
			return true
		}
	}
	return false
}

// StripSandboxHost returns the canonical (non-sandbox) host for host by
// removing its first "sandbox" label, keeping the port:
// develop.sandbox.api.example.com → develop.api.example.com,
// sandbox.localhost:8080 → localhost:8080. A host without the label is
// returned unchanged.
func StripSandboxHost(host string) string {
	name, port := hostname(host), ""
	if len(name) < len(host) {
		port = host[len(name):]
	}
	labels := strings.Split(name, ".")
	for i, l := range labels {
		if strings.EqualFold(l, sandboxLabel) {
			return strings.Join(append(labels[:i:i], labels[i+1:]...), ".") + port
		}
	}
	return host
}

// hostname drops the ":port" suffix, if any.
func hostname(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}
