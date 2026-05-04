package ingester

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultListenAddr     = ":8082"
	defaultMinIOEndpoint  = "minio:9000"
	defaultMinIOAccessKey = "dropcheck"
	defaultMinIOSecretKey = "dropcheck-secret"
	defaultMinIOBucket    = "dropcheck"
	defaultObjectSuffix   = ".pb"
	defaultPushgatewayURL = "http://pushgateway:9091"
	defaultPushJob        = "dropcheck_festival_results"
	defaultBatchInterval  = time.Minute
	defaultMaxObjectBytes = 64 << 20
)

// Config controls the Festival Results ingester runtime.
type Config struct {
	ListenAddr     string
	MinIOEndpoint  string
	MinIOAccessKey string
	MinIOSecretKey string
	MinIOBucket    string
	MinIOPrefix    string
	MinIOUseSSL    bool
	ObjectSuffix   string
	PushgatewayURL string
	PushJob        string
	BatchInterval  time.Duration
	MaxObjectBytes int64
}

// ConfigFromEnv loads runtime configuration from environment variables.
func ConfigFromEnv() (Config, error) {
	useSSL, err := envBoolAny([]string{"DROPCHECK_INGESTER_MINIO_SECURE", "DROPCHECK_MINIO_USE_SSL"}, false)
	if err != nil {
		return Config{}, err
	}
	interval, err := envDurationAny([]string{"DROPCHECK_INGESTER_POLL_INTERVAL", "DROPCHECK_BATCH_INTERVAL"}, defaultBatchInterval)
	if err != nil {
		return Config{}, err
	}
	maxObjectBytes, err := envInt64("DROPCHECK_INGESTER_MAX_OBJECT_BYTES", defaultMaxObjectBytes)
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		ListenAddr:     envOr("DROPCHECK_INGESTER_ADDR", defaultListenAddr),
		MinIOEndpoint:  envOrAny([]string{"DROPCHECK_INGESTER_MINIO_ENDPOINT", "DROPCHECK_MINIO_ENDPOINT"}, defaultMinIOEndpoint),
		MinIOAccessKey: envOrAny([]string{"DROPCHECK_INGESTER_MINIO_ACCESS_KEY", "DROPCHECK_MINIO_ACCESS_KEY"}, defaultMinIOAccessKey),
		MinIOSecretKey: envOrAny([]string{"DROPCHECK_INGESTER_MINIO_SECRET_KEY", "DROPCHECK_MINIO_SECRET_KEY"}, defaultMinIOSecretKey),
		MinIOBucket:    envOrAny([]string{"DROPCHECK_INGESTER_MINIO_BUCKET", "DROPCHECK_MINIO_BUCKET"}, defaultMinIOBucket),
		MinIOPrefix:    strings.Trim(strings.TrimSpace(envOrAny([]string{"DROPCHECK_INGESTER_MINIO_PREFIX", "DROPCHECK_MINIO_PREFIX"}, "")), "/"),
		MinIOUseSSL:    useSSL,
		ObjectSuffix:   envOrAny([]string{"DROPCHECK_INGESTER_OBJECT_SUFFIX", "DROPCHECK_OBJECT_SUFFIX"}, defaultObjectSuffix),
		PushgatewayURL: strings.TrimRight(envOrAny([]string{"DROPCHECK_INGESTER_PUSHGATEWAY_URL", "DROPCHECK_PUSHGATEWAY_URL"}, defaultPushgatewayURL), "/"),
		PushJob:        envOrAny([]string{"DROPCHECK_INGESTER_JOB", "DROPCHECK_PUSH_JOB"}, defaultPushJob),
		BatchInterval:  interval,
		MaxObjectBytes: maxObjectBytes,
	}
	if cfg.MinIOEndpoint == "" {
		return Config{}, fmt.Errorf("DROPCHECK_MINIO_ENDPOINT is required")
	}
	if cfg.MinIOBucket == "" {
		return Config{}, fmt.Errorf("DROPCHECK_MINIO_BUCKET is required")
	}
	if cfg.PushgatewayURL == "" {
		return Config{}, fmt.Errorf("DROPCHECK_PUSHGATEWAY_URL is required")
	}
	if cfg.PushJob == "" {
		return Config{}, fmt.Errorf("DROPCHECK_PUSH_JOB is required")
	}
	if cfg.BatchInterval <= 0 {
		return Config{}, fmt.Errorf("DROPCHECK_INGESTER_POLL_INTERVAL must be positive")
	}
	if cfg.MaxObjectBytes <= 0 {
		return Config{}, fmt.Errorf("DROPCHECK_INGESTER_MAX_OBJECT_BYTES must be positive")
	}
	return cfg, nil
}

func envOr(name, fallback string) string {
	return envOrAny([]string{name}, fallback)
}

func envOrAny(names []string, fallback string) string {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return fallback
}

func envBool(name string, fallback bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s: %w", name, err)
	}
	return parsed, nil
}

func envBoolAny(names []string, fallback bool) (bool, error) {
	for _, name := range names {
		value := strings.TrimSpace(os.Getenv(name))
		if value == "" {
			continue
		}
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return false, fmt.Errorf("%s: %w", name, err)
		}
		return parsed, nil
	}
	return fallback, nil
}

func envDuration(name string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return parsed, nil
}

func envDurationAny(names []string, fallback time.Duration) (time.Duration, error) {
	for _, name := range names {
		value := strings.TrimSpace(os.Getenv(name))
		if value == "" {
			continue
		}
		parsed, err := time.ParseDuration(value)
		if err != nil {
			return 0, fmt.Errorf("%s: %w", name, err)
		}
		return parsed, nil
	}
	return fallback, nil
}

func envInt64(name string, fallback int64) (int64, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return parsed, nil
}
