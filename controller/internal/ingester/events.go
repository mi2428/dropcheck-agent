package ingester

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// ObjectEvent identifies a MinIO object notification target.
type ObjectEvent struct {
	Bucket string
	Key    string
	ETag   string
	Size   int64
}

type minIONotification struct {
	Records []minIORecord `json:"Records"`
}

type minIORecord struct {
	EventName string `json:"eventName"`
	S3        struct {
		Bucket struct {
			Name string `json:"name"`
		} `json:"bucket"`
		Object struct {
			Key  string `json:"key"`
			ETag string `json:"eTag"`
			Size int64  `json:"size"`
		} `json:"object"`
	} `json:"s3"`
}

// ParseMinIONotification returns object-created events from a MinIO webhook payload.
func ParseMinIONotification(data []byte) ([]ObjectEvent, error) {
	var payload minIONotification
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("decode minio notification: %w", err)
	}
	events := make([]ObjectEvent, 0, len(payload.Records))
	for _, record := range payload.Records {
		if !strings.HasPrefix(record.EventName, "s3:ObjectCreated:") {
			continue
		}
		key := decodeObjectKey(record.S3.Object.Key)
		if key == "" {
			continue
		}
		events = append(events, ObjectEvent{
			Bucket: record.S3.Bucket.Name,
			Key:    key,
			ETag:   record.S3.Object.ETag,
			Size:   record.S3.Object.Size,
		})
	}
	return events, nil
}

func decodeObjectKey(key string) string {
	if decoded, err := url.QueryUnescape(key); err == nil {
		return decoded
	}
	return key
}
