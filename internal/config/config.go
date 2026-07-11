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

	// Edge Application Management (Kubernetes backend) configuration.
	// EAMEnabled turns on the /apps, /deployments and /clusters endpoints.
	EAMEnabled bool
	// Kubeconfig is the path to a kubeconfig file; empty uses in-cluster config.
	Kubeconfig string
	// K8sNamespacePrefix prefixes namespaces created for deployments.
	K8sNamespacePrefix string
	// MEPEnabled turns on OAI MEP mp1 service registration for deployments.
	MEPEnabled bool
	// MEPBaseURL is the OAI MEP service registry base URL.
	MEPBaseURL string
}

func Load() *Config {
	return &Config{
		ServerPort:    getEnv("SERVER_PORT", "8080"),
		NEFBaseURL:    getEnv("NEF_BASE_URL", "http://localhost:8000"),
		NEFToken:      getEnv("NEF_TOKEN", ""),
		CAMARAAuthURL: getEnv("CAMARA_AUTH_URL", ""),
		LogLevel:      getEnv("LOG_LEVEL", "info"),
		PprofPort:     getEnv("PPROF_PORT", ""),

		EAMEnabled:         getEnv("EAM_ENABLED", "false") == "true",
		Kubeconfig:         getEnv("KUBECONFIG", ""),
		K8sNamespacePrefix: getEnv("K8S_NAMESPACE_PREFIX", "ctrail-"),
		MEPEnabled:         getEnv("MEP_ENABLED", "false") == "true",
		MEPBaseURL:         getEnv("MEP_BASE_URL", "http://oai-mep.org/service_registry/v1"),
	}
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}
