package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// An explicit GOOGLE_CLOUD_PROJECT_ID always wins over the metadata server.
// This is what keeps local development working (no metadata server there) and
// what lets a service be pointed at another project without a code change.
func TestResolveGoogleCloudProjectID_ExplicitValueWins(t *testing.T) {
	t.Setenv(googleCloudProjectIDEnv, "explicit-project")

	resolveGoogleCloudProjectID()

	assert.Equal(t, "explicit-project", os.Getenv(googleCloudProjectIDEnv))
}

// Off GCP with nothing set, the variable stays empty and startup continues.
// A missing project degrades cloud logging / secret manager / tasks / pub-sub,
// but it must not stop the process — that is how every developer runs locally.
//
// The test machine is not on GCP, so this exercises the real OnGCE branch.
func TestResolveGoogleCloudProjectID_StaysEmptyOffGCP(t *testing.T) {
	t.Setenv(googleCloudProjectIDEnv, "")

	assert.NotPanics(t, func() { resolveGoogleCloudProjectID() })
	assert.Empty(t, os.Getenv(googleCloudProjectIDEnv))
}

// The whole point of exporting the value (rather than only filling the config
// struct) is that go-kiban reads it with os.Getenv at call time, from four
// different services. Guard that contract: whatever resolution ends up doing,
// the variable has to be the channel.
func TestResolveGoogleCloudProjectID_ExportsToTheEnvironment(t *testing.T) {
	t.Setenv(googleCloudProjectIDEnv, "from-somewhere")

	resolveGoogleCloudProjectID()

	// os.Getenv is what go-kiban's Secret Manager, logger, Cloud Tasks and
	// Pub/Sub call — not the parsed config struct.
	assert.Equal(t, "from-somewhere", os.Getenv(googleCloudProjectIDEnv))
}
