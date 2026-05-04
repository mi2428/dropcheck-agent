package ingester

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
)

const notificationBodyLimit = 1 << 20

type bucketNotification struct {
	Records []notificationRecord `json:"Records"`
}

type notificationRecord struct {
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

// DecodeNotification extracts candidate object keys from a MinIO/S3 event body.
func DecodeNotification(r io.Reader, suffix string) ([]ObjectRef, error) {
	var payload bucketNotification
	decoder := json.NewDecoder(io.LimitReader(r, notificationBodyLimit))
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode notification: %w", err)
	}
	var objects []ObjectRef
	for _, record := range payload.Records {
		if !strings.HasPrefix(record.EventName, "s3:ObjectCreated:") {
			continue
		}
		key := decodeObjectKey(record.S3.Object.Key)
		if suffix != "" && !strings.HasSuffix(key, suffix) {
			continue
		}
		if key == "" {
			continue
		}
		objects = append(objects, ObjectRef{
			Bucket: record.S3.Bucket.Name,
			Key:    key,
			ETag:   record.S3.Object.ETag,
			Size:   record.S3.Object.Size,
		})
	}
	return objects, nil
}

func decodeObjectKey(key string) string {
	decoded, err := url.QueryUnescape(key)
	if err != nil {
		return key
	}
	return decoded
}
