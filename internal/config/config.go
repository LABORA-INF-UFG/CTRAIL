package config

import (
	"os"
)

type Config struct {
	ServerPort    string
	NEFBaseURL    string
	NEFToken      string
	CAMARAAuthURL string
	LogLevel      string
	PprofPort     string
}

func Load() *Config {
	return &Config{
		ServerPort:    getEnv("SERVER_PORT", "8080"),
		NEFBaseURL:    getEnv("NEF_BASE_URL", "http://localhost:8000"),
		NEFToken:      getEnv("NEF_TOKEN", ""),
		CAMARAAuthURL: getEnv("CAMARA_AUTH_URL", ""),
		LogLevel:      getEnv("LOG_LEVEL", "info"),
		PprofPort:     getEnv("PPROF_PORT", ""),
	}
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}
