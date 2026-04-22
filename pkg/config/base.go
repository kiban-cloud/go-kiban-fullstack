package config

import (
	"os"

	"github.com/kiban-cloud/go-kiban-fullstack/pkg/domain/commons"

	"github.com/caarlos0/env/v6"
	"github.com/joho/godotenv"
	errorsWrapper "github.com/pkg/errors"
)

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

// LoadEnv loads .env (skipped when running in Cloud Run) and parses env vars
// into cfg via caarlos0/env. It panics on any error because a misconfigured
// app should fail to start.
func LoadEnv(cfg any) {
	if os.Getenv("K_REVISION") == "" {
		if err := godotenv.Load(".env"); err != nil {
			panic(errorsWrapper.Wrap(err, "error loading .env file"))
		}
	}

	if err := env.Parse(cfg); err != nil {
		panic(errorsWrapper.Wrap(err, "error parsing environment variables"))
	}
}
