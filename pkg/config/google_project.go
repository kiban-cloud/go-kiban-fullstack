package config

import (
	"context"
	"log"
	"os"
	"time"

	"cloud.google.com/go/compute/metadata"
)

// googleCloudProjectIDEnv is the variable every kiban service reads to know
// which GCP project it runs in. go-kiban resolves it with os.Getenv at call
// time (Secret Manager, logger, Cloud Tasks, Pub/Sub), so exporting it once at
// boot is enough for every consumer — none of them read it during init.
const googleCloudProjectIDEnv = "GOOGLE_CLOUD_PROJECT_ID"

// metadataLookupTimeout bounds the metadata-server call. On GCP it answers off
// the local link in milliseconds; the timeout only matters when OnGCE guessed
// wrong, and it must stay short because this runs on the startup path.
const metadataLookupTimeout = 2 * time.Second

// resolveGoogleCloudProjectID fills GOOGLE_CLOUD_PROJECT_ID from the metadata
// server when it wasn't supplied, so the project no longer has to be wired by
// hand per environment.
//
// Resolution order:
//
//  1. The env var, when set — an explicit value always wins, which keeps the
//     local .env working and leaves an escape hatch for pointing a service at
//     another project.
//  2. metadata.ProjectID(), when running on GCP.
//  3. Nothing: log and carry on. A missing project ID degrades the features
//     that need it (cloud logging, Secret Manager, tasks, pub/sub) but is not
//     fatal — local development runs this way today.
//
// Called from LoadEnv before parsing, so the parsed config and every later
// os.Getenv see the same value.
//
// Logging here is a bare log.Printf on purpose: this is boot code, there is no
// request to correlate against, and the logger isn't wired yet.
func resolveGoogleCloudProjectID() {
	if os.Getenv(googleCloudProjectIDEnv) != "" {
		return
	}

	if !metadata.OnGCE() {
		log.Printf("%s is not set and we are not running on GCP: "+
			"features that need a project (cloud logging, secret manager, "+
			"cloud tasks, pub/sub) will be unavailable", googleCloudProjectIDEnv)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), metadataLookupTimeout)
	defer cancel()

	projectID, err := metadata.ProjectIDWithContext(ctx)
	if err != nil || projectID == "" {
		log.Printf("could not read the project id from the metadata server, "+
			"leaving %s empty: %v", googleCloudProjectIDEnv, err)
		return
	}

	if err := os.Setenv(googleCloudProjectIDEnv, projectID); err != nil {
		log.Printf("could not export %s=%s: %v", googleCloudProjectIDEnv, projectID, err)
		return
	}
	log.Printf("%s resolved from the metadata server: %s", googleCloudProjectIDEnv, projectID)
}
