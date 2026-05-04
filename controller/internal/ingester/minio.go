package ingester

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// ObjectRef identifies a MinIO object that may contain a standalone run archive.
type ObjectRef struct {
	// Bucket is the source bucket name when supplied by notifications.
	Bucket string
	// Key is the object key inside the bucket.
	Key string
	// ETag is the object entity tag used for in-memory deduplication.
	ETag string
	// Size is the object size in bytes used for in-memory deduplication.
	Size int64
}

func (o ObjectRef) signature() string {
	switch {
	case o.ETag != "" && o.Size >= 0:
		return o.ETag + "|" + fmt.Sprint(o.Size)
	case o.ETag != "":
		return o.ETag
	case o.Size >= 0:
		return fmt.Sprint(o.Size)
	default:
		return ""
	}
}

// ObjectStore reads standalone result objects from MinIO or a compatible store.
type ObjectStore interface {
	// GetObject returns the full object payload for key.
	GetObject(ctx context.Context, key string) ([]byte, error)
	// ListObjects returns all candidate objects under the configured prefix.
	ListObjects(ctx context.Context) ([]ObjectRef, error)
}

// MinIOStore is an ObjectStore backed by MinIO's S3-compatible API.
type MinIOStore struct {
	client *minio.Client
	bucket string
	prefix string
	suffix string
	limit  int64
}

// NewMinIOStore creates an ObjectStore backed by MinIO's S3-compatible client.
func NewMinIOStore(cfg Config) (*MinIOStore, error) {
	limit := cfg.MaxObjectBytes
	if limit <= 0 {
		limit = defaultMaxObjectBytes
	}
	client, err := minio.New(cfg.MinIOEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.MinIOAccessKey, cfg.MinIOSecretKey, ""),
		Secure: cfg.MinIOUseSSL,
	})
	if err != nil {
		return nil, err
	}
	return &MinIOStore{
		client: client,
		bucket: cfg.MinIOBucket,
		prefix: cfg.MinIOPrefix,
		suffix: cfg.ObjectSuffix,
		limit:  limit,
	}, nil
}

// GetObject reads one object and enforces the configured maximum object size.
func (s *MinIOStore) GetObject(ctx context.Context, key string) ([]byte, error) {
	object, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer object.Close()
	data, err := io.ReadAll(io.LimitReader(object, s.limit+1))
	if err != nil {
		return nil, fmt.Errorf("read object %q: %w", key, err)
	}
	if int64(len(data)) > s.limit {
		return nil, fmt.Errorf("object %q exceeds max read size %d", key, s.limit)
	}
	return data, nil
}

// ListObjects returns matching objects under the store prefix.
func (s *MinIOStore) ListObjects(ctx context.Context) ([]ObjectRef, error) {
	var objects []ObjectRef
	for info := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{
		Prefix:    s.prefix,
		Recursive: true,
	}) {
		if info.Err != nil {
			return nil, info.Err
		}
		if !objectMatches(info.Key, s.prefix, s.suffix) {
			continue
		}
		objects = append(objects, ObjectRef{
			Bucket: s.bucket,
			Key:    info.Key,
			ETag:   info.ETag,
			Size:   info.Size,
		})
	}
	return objects, nil
}

func objectMatches(key, prefix, suffix string) bool {
	if key == "" {
		return false
	}
	prefix = strings.Trim(prefix, "/")
	if prefix != "" && key != prefix && !strings.HasPrefix(key, prefix+"/") {
		return false
	}
	if suffix != "" && !strings.HasSuffix(key, suffix) {
		return false
	}
	return true
}
