package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testConfig struct {
	Env    string `env:"ENV,required"`
	Secret string `env:"SECRET_TOKEN,required"`
	Plain  string `env:"PLAIN_VALUE"`
}

// isolateEnv snapshots the process environment and restores it exactly after
// the test, so godotenv mutations from one test never leak into another.
func isolateEnv(t *testing.T) {
	t.Helper()
	saved := os.Environ()
	t.Cleanup(func() {
		os.Clearenv()
		for _, kv := range saved {
			if i := strings.IndexByte(kv, '='); i >= 0 {
				_ = os.Setenv(kv[:i], kv[i+1:])
			}
		}
	})
	os.Unsetenv("ENV")
	os.Unsetenv("K_REVISION")
}

func writeEnvFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
}

func recoverErr(t *testing.T, fn func()) error {
	t.Helper()
	var recovered error
	func() {
		defer func() {
			if r := recover(); r != nil {
				err, ok := r.(error)
				require.True(t, ok, "panic value should be an error, got %T", r)
				recovered = err
			}
		}()
		fn()
	}()
	return recovered
}

func TestLoadEnv_DefaultsToDevelopEnvFileWhenEnvUnset(t *testing.T) {
	isolateEnv(t)
	dir := t.TempDir()
	writeEnvFile(t, dir, "develop.env", "ENV=develop\nSECRET_TOKEN=fromfile\nPLAIN_VALUE=base\n")
	t.Chdir(dir)

	var cfg testConfig
	LoadEnv(&cfg)

	assert.Equal(t, "develop", cfg.Env)
	assert.Equal(t, "fromfile", cfg.Secret)
	assert.Equal(t, "base", cfg.Plain)
}

func TestLoadEnv_SelectsEnvFileFromENVShellVar(t *testing.T) {
	isolateEnv(t)
	dir := t.TempDir()
	writeEnvFile(t, dir, "develop.env", "SECRET_TOKEN=devsecret\n")   // must NOT be loaded
	writeEnvFile(t, dir, "hotfix.env", "ENV=hotfix\nSECRET_TOKEN=hotfixsecret\n")
	t.Chdir(dir)
	os.Setenv("ENV", "hotfix")

	var cfg testConfig
	LoadEnv(&cfg)

	assert.Equal(t, "hotfix", cfg.Env)
	assert.Equal(t, "hotfixsecret", cfg.Secret)
}

func TestLoadEnv_SecretsFileTakesPrecedenceOverBase(t *testing.T) {
	isolateEnv(t)
	dir := t.TempDir()
	writeEnvFile(t, dir, "develop.env", "ENV=develop\nSECRET_TOKEN="+secretPlaceholder+"\nPLAIN_VALUE=base\n")
	writeEnvFile(t, dir, "secrets.develop.env", "SECRET_TOKEN=realsecret\n")
	t.Chdir(dir)

	var cfg testConfig
	LoadEnv(&cfg)

	assert.Equal(t, "realsecret", cfg.Secret, "gitignored secrets.<env>.env must win (first-wins)")
	assert.Equal(t, "base", cfg.Plain)
}

func TestLoadEnv_PanicsWhenSecretPlaceholderNotFilled(t *testing.T) {
	isolateEnv(t)
	dir := t.TempDir()
	writeEnvFile(t, dir, "develop.env", "ENV=develop\nSECRET_TOKEN="+secretPlaceholder+"\n")
	t.Chdir(dir)

	err := recoverErr(t, func() { LoadEnv(&testConfig{}) })

	require.Error(t, err, "unfilled placeholder must panic")
	assert.Contains(t, err.Error(), "SECRET_TOKEN")
	assert.Contains(t, err.Error(), "placeholder")
}

func TestLoadEnv_SkipsFilesWhenRunningInCloudRun(t *testing.T) {
	isolateEnv(t)
	dir := t.TempDir()
	writeEnvFile(t, dir, "develop.env", "SECRET_TOKEN=fromfile\n") // must be ignored in Cloud Run
	t.Chdir(dir)
	os.Setenv("K_REVISION", "svc-00042-abc")
	os.Setenv("ENV", "production")
	os.Setenv("SECRET_TOKEN", "fromrevision")

	var cfg testConfig
	LoadEnv(&cfg)

	assert.Equal(t, "production", cfg.Env)
	assert.Equal(t, "fromrevision", cfg.Secret, "Cloud Run reads injected env, not files")
}

func TestLoadEnv_PanicsOnMissingRequiredVar(t *testing.T) {
	isolateEnv(t)
	t.Chdir(t.TempDir()) // no env files; a missing <env>.env only logs, then Parse must fail

	err := recoverErr(t, func() { LoadEnv(&testConfig{}) })

	require.Error(t, err)
	assert.Contains(t, err.Error(), "error parsing environment variables")
}
