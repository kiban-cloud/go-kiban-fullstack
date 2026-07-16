package config

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/kiban-cloud/go-kiban-fullstack/pkg/domain/commons"

	"github.com/caarlos0/env/v6"
	"github.com/joho/godotenv"
	errorsWrapper "github.com/pkg/errors"
)

// defaultLocalEnv selects which committed <env>.env is loaded when ENV is not
// exported in the shell.
const defaultLocalEnv = "develop"

// secretPlaceholder marks a committed key in <env>.env whose real value must be
// supplied out-of-band via secrets.<env>.env (gitignored). Startup fails fast
// if any survives — i.e. the developer forgot to fill their local secrets.
const secretPlaceholder = "__FILL_IN_secrets.env__"

type Base struct {
	Env                  commons.ENV       `env:"ENV,required"`
	Port                 int               `env:"PORT,required"`
	MongoURI             string            `env:"MONGO_URI,required"`
	GoogleCloudProjectID string            `env:"GOOGLE_CLOUD_PROJECT_ID"`
	GoogleCloudRevision  string            `env:"K_REVISION"`
	JwtSecretKey         string            `env:"JWT_SECRET_KEY,required"`
	EmailAPIKey          string            `env:"EMAIL_API_KEY,required"`
	EmailFromEmail       string            `env:"EMAIL_FROM_EMAIL,required"`
	EmailFromName        string            `env:"EMAIL_FROM_NAME,required"`
	FrontendURL          string            `env:"FRONTEND_URL,required"`
	LogLevel             commons.LOG_LEVEL `env:"LOG_LEVEL,required"`
}

func (c *Base) IsRunningInCloudRun() bool {
	return c.GoogleCloudRevision != ""
}

func (c *Base) Validate() {
	c.Env.Validate()
	c.LogLevel.Validate()
}

// LoadEnv populates cfg from the environment and panics on any misconfiguration,
// because a badly-configured app must fail to start.
//
// In Cloud Run (K_REVISION is set) the revision injects the env vars, so no
// files are read. Locally, ENV (default "develop") selects which committed
// <env>.env to load, layered under gitignored secrets files that take
// precedence — godotenv is first-wins and never overrides a var already set in
// the shell:
//
//	1. secrets.<env>.env — per-developer real secrets.
//	2. secrets.env       — shared local secrets (optional).
//	3. <env>.env         — committed base: non-sensitive values + placeholders.
func LoadEnv(cfg any) {
	if os.Getenv("K_REVISION") == "" {
		loadLocalEnvFiles()
	}

	if err := env.Parse(cfg); err != nil {
		panic(errorsWrapper.Wrap(err, "error parsing environment variables"))
	}
}

// loadLocalEnvFiles layers the local env files for the target ENV and stops the
// process if any secret still holds the placeholder.
func loadLocalEnvFiles() {
	// ENV is read from the shell, not from any .env file — the file is what
	// defines the rest of the vars, so it cannot select itself. Defaults to develop.
	targetEnv := strings.TrimSpace(os.Getenv("ENV"))
	if targetEnv == "" {
		targetEnv = defaultLocalEnv
	}

	_ = godotenv.Load(fmt.Sprintf("secrets.%s.env", targetEnv))
	_ = godotenv.Load("secrets.env")
	if err := godotenv.Load(fmt.Sprintf("%s.env", targetEnv)); err != nil {
		log.Printf("could not load %s.env: %v", targetEnv, err)
	}

	assertNoSecretPlaceholders(targetEnv)
}

// assertNoSecretPlaceholders panics if any env var still holds the placeholder,
// meaning the developer forgot to fill secrets.<env>.env.
func assertNoSecretPlaceholders(targetEnv string) {
	var missing []string
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i >= 0 && kv[i+1:] == secretPlaceholder {
			missing = append(missing, kv[:i])
		}
	}
	if len(missing) > 0 {
		panic(errorsWrapper.New(fmt.Sprintf(
			"missing secrets for ENV=%s: %s still set to the placeholder; "+
				"copy secrets.env.example to secrets.%s.env and fill them in",
			targetEnv, strings.Join(missing, ", "), targetEnv)))
	}
}
