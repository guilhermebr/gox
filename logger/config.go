package logger

type Config struct {
	Level       string `conf:"env:LOGGING_LEVEL,default:info"`
	Type        string `conf:"env:LOGGING_TYPE,default:text"`
	Stderr      bool   `conf:"env:LOGGING_STDERR,default:false"`
	Environment string `conf:"env:ENVIRONMENT,default:development"`
}
