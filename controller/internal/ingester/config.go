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
	defaultPushgatewayURL = "http://pushgateway:9091"
	defaultJobName        = "dropcheck_festival"
	defaultPollInterval   = time.Minute
	defaultMaxObjectBytes = 64 << 20
	defaultQueueSize      = 128
	defaultWorkers        = 2
)

// Config contains runtime settings for the Festival results ingester.
type Config struct {
	ListenAddr     string
	MinIOEndpoint  string
	MinIOAccessKey string
	MinIOSecretKey string
	MinIOBucket    string
	MinIOPrefix    string
	MinIOSecure    bool
	PushgatewayURL string
	JobName        string
	PollInterval   time.Duration
	MaxObjectBytes int64
	QueueSize      int
	Workers        int
}

// ConfigFromEnv builds Config from DROPCHECK_INGESTER_* environment variables.
func ConfigFromEnv() (Config, error) {
	cfg := Config{
		ListenAddr:     envString("DROPCHECK_INGESTER_ADDR", defaultListenAddr),
		MinIOEndpoint:  envString("DROPCHECK_INGESTER_MINIO_ENDPOINT", defaultMinIOEndpoint),
		MinIOAccessKey: envString("DROPCHECK_INGESTER_MINIO_ACCESS_KEY", defaultMinIOAccessKey),
		MinIOSecretKey: envString("DROPCHECK_INGESTER_MINIO_SECRET_KEY", defaultMinIOSecretKey),
		MinIOBucket:    envString("DROPCHECK_INGESTER_MINIO_BUCKET", defaultMinIOBucket),
		MinIOPrefix:    strings.Trim(strings.TrimSpace(os.Getenv("DROPCHECK_INGESTER_MINIO_PREFIX")), "/"),
		PushgatewayURL: strings.TrimRight(envString("DROPCHECK_INGESTER_PUSHGATEWAY_URL", defaultPushgatewayURL), "/"),
		JobName:        envString("DROPCHECK_INGESTER_JOB", defaultJobName),
		PollInterval:   defaultPollInterval,
		MaxObjectBytes: defaultMaxObjectBytes,
		QueueSize:      defaultQueueSize,
		Workers:        defaultWorkers,
	}
	var err error
	if cfg.MinIOSecure, err = envBool("DROPCHECK_INGESTER_MINIO_SECURE", false); err != nil {
		return Config{}, err
	}
	if cfg.PollInterval, err = envDuration("DROPCHECK_INGESTER_POLL_INTERVAL", defaultPollInterval); err != nil {
		return Config{}, err
	}
	if cfg.MaxObjectBytes, err = envInt64("DROPCHECK_INGESTER_MAX_OBJECT_BYTES", defaultMaxObjectBytes); err != nil {
		return Config{}, err
	}
	if cfg.QueueSize, err = envInt("DROPCHECK_INGESTER_QUEUE_SIZE", defaultQueueSize); err != nil {
		return Config{}, err
	}
	if cfg.Workers, err = envInt("DROPCHECK_INGESTER_WORKERS", defaultWorkers); err != nil {
		return Config{}, err
	}
	if cfg.ListenAddr == "" {
		return Config{}, fmt.Errorf("DROPCHECK_INGESTER_ADDR is empty")
	}
	if cfg.MinIOEndpoint == "" {
		return Config{}, fmt.Errorf("DROPCHECK_INGESTER_MINIO_ENDPOINT is empty")
	}
	if cfg.MinIOBucket == "" {
		return Config{}, fmt.Errorf("DROPCHECK_INGESTER_MINIO_BUCKET is empty")
	}
	if cfg.PushgatewayURL == "" {
		return Config{}, fmt.Errorf("DROPCHECK_INGESTER_PUSHGATEWAY_URL is empty")
	}
	if cfg.JobName == "" {
		return Config{}, fmt.Errorf("DROPCHECK_INGESTER_JOB is empty")
	}
	if cfg.PollInterval <= 0 {
		return Config{}, fmt.Errorf("DROPCHECK_INGESTER_POLL_INTERVAL must be positive")
	}
	if cfg.MaxObjectBytes <= 0 {
		return Config{}, fmt.Errorf("DROPCHECK_INGESTER_MAX_OBJECT_BYTES must be positive")
	}
	if cfg.QueueSize <= 0 {
		return Config{}, fmt.Errorf("DROPCHECK_INGESTER_QUEUE_SIZE must be positive")
	}
	if cfg.Workers <= 0 {
		return Config{}, fmt.Errorf("DROPCHECK_INGESTER_WORKERS must be positive")
	}
	return cfg, nil
}

func envString(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
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

func envInt(name string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return parsed, nil
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

func envDuration(name string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err == nil {
		return parsed, nil
	}
	millis, intErr := strconv.ParseInt(value, 10, 64)
	if intErr != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return time.Duration(millis) * time.Millisecond, nil
}
