package ingester

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"

	"github.com/minio/minio-go/v7/pkg/notification"
)

const notificationBodyLimit = 1 << 20

var errNotificationTooLarge = errors.New("notification exceeds 1 MiB limit")

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
			Size *int64 `json:"size"`
		} `json:"object"`
	} `json:"s3"`
}

// DecodeNotification extracts candidate object keys from a MinIO/S3 event body.
func DecodeNotification(r io.Reader, cfg Config) ([]ObjectRef, error) {
	data, err := io.ReadAll(io.LimitReader(r, notificationBodyLimit+1))
	if err != nil {
		return nil, fmt.Errorf("read notification: %w", err)
	}
	if len(data) > notificationBodyLimit {
		return nil, errNotificationTooLarge
	}
	var payload bucketNotification
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("decode notification: %w", err)
	}
	if len(payload.Records) == 0 {
		return nil, fmt.Errorf("notification must contain records")
	}
	var objects []ObjectRef
	for index, record := range payload.Records {
		created := true
		switch notification.EventType(record.EventName) {
		case notification.ObjectCreatedPut, notification.ObjectCreatedPost, notification.ObjectCreatedCopy,
			notification.ObjectCreatedCompleteMultipartUpload, notification.ObjectCreatedDeleteTagging,
			notification.ObjectCreatedPutLegalHold, notification.ObjectCreatedPutRetention, notification.ObjectCreatedPutTagging:
		case notification.ObjectRemovedDelete, notification.ObjectRemovedDeleteMarkerCreated:
			created = false
		default:
			return nil, fmt.Errorf("record %d: invalid event name", index)
		}
		key, err := url.QueryUnescape(record.S3.Object.Key)
		if err != nil {
			return nil, fmt.Errorf("record %d: invalid object key encoding", index)
		}
		object := ObjectRef{Bucket: record.S3.Bucket.Name, Key: key, ETag: record.S3.Object.ETag}
		if record.S3.Object.Size != nil {
			object.Size = *record.S3.Object.Size
		} else if created {
			return nil, fmt.Errorf("record %d: missing object size", index)
		}
		if err := validateObject(cfg, object); err != nil {
			return nil, fmt.Errorf("record %d: %w", index, err)
		}
		if created && objectMatches(key, cfg.MinIOPrefix, cfg.ObjectSuffix) {
			if err := validateArchiveSize(cfg, object); err != nil {
				return nil, fmt.Errorf("record %d: %w", index, err)
			}
			objects = append(objects, object)
		}
	}
	return objects, nil
}
