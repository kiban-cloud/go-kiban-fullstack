package commons

import (
	"fmt"

	errorsWrapper "github.com/pkg/errors"
)

type ENV string

var ENVS = struct {
	DEV  ENV
	PROD ENV
}{
	DEV:  "develop",
	PROD: "production",
}

type LOG_LEVEL string

var LOG_LEVELS = struct {
	DEBUG LOG_LEVEL
	INFO  LOG_LEVEL
	WARN  LOG_LEVEL
	ERROR LOG_LEVEL
}{
	DEBUG: "debug",
	INFO:  "info",
	WARN:  "warn",
	ERROR: "error",
}

func (e ENV) Validate() {
	switch e {
	case ENVS.DEV, ENVS.PROD:
		return
	default:
		panic(errorsWrapper.New(fmt.Sprintf("invalid ENV variable value: %s", e)))
	}
}

func (l LOG_LEVEL) Validate() {
	switch l {
	case LOG_LEVELS.DEBUG, LOG_LEVELS.INFO, LOG_LEVELS.WARN, LOG_LEVELS.ERROR:
		return
	default:
		panic(errorsWrapper.New(fmt.Sprintf("invalid LOG_LEVEL variable value: %s", l)))
	}
}
