package ingester

import (
	"context"
	"fmt"
	"io"
	"iter"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// ObjectRef identifies a MinIO object that may contain a standalone run archive.
type ObjectRef struct {
	// Bucket is the required source bucket for notifications and trusted listings.
	Bucket string
	// Key is the object key inside the bucket.
	Key string
	// ETag is the object entity tag used for in-memory deduplication.
	ETag string
	// Size is the object size in bytes used for in-memory deduplication.
	Size int64
}

func validateObject(cfg Config, object ObjectRef) error {
	if cfg.MinIOBucket == "" || object.Bucket != cfg.MinIOBucket {
		return fmt.Errorf("object bucket must match the configured bucket")
	}
	if object.Key == "" || len(object.Key) > 1024 || !utf8.ValidString(object.Key) || strings.ContainsFunc(object.Key, unicode.IsControl) {
		return fmt.Errorf("object key must be 1-1024 UTF-8 bytes without control characters")
	}
	if object.Size < 0 {
		return fmt.Errorf("object size must be nonnegative")
	}
	return nil
}

// validateArchiveSize applies the read cap only after archive/event filtering.
func validateArchiveSize(cfg Config, object ObjectRef) error {
	limit := cfg.MaxObjectBytes
	if limit <= 0 {
		limit = defaultMaxObjectBytes
	}
	if object.Size > limit {
		return fmt.Errorf("archive object exceeds max read size %d", limit)
	}
	return nil
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
	// GetObject returns the full object payload for key and must honor ctx.
	GetObject(ctx context.Context, key string) ([]byte, error)
	// ListObjects yields candidate objects under the configured prefix, or an
	// error if enumeration cannot complete. It must honor ctx and early iteration exit.
	ListObjects(ctx context.Context) iter.Seq2[ObjectRef, error]
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
	defer func() { _ = object.Close() }()
	data, err := io.ReadAll(io.LimitReader(object, s.limit+1))
	if err != nil {
		return nil, fmt.Errorf("read object %q: %w", key, err)
	}
	if int64(len(data)) > s.limit {
		return nil, fmt.Errorf("object %q exceeds max read size %d", key, s.limit)
	}
	return data, nil
}

// ListObjects yields matching objects under the store prefix without retaining
// the complete listing. Early exit cancels the SDK's listing goroutine.
func (s *MinIOStore) ListObjects(ctx context.Context) iter.Seq2[ObjectRef, error] {
	return func(yield func(ObjectRef, error) bool) {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		for info := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{
			Prefix:    s.prefix,
			Recursive: true,
		}) {
			if info.Err != nil {
				yield(ObjectRef{}, info.Err)
				return
			}
			if !objectMatches(info.Key, s.prefix, s.suffix) {
				continue
			}
			if !yield(ObjectRef{Bucket: s.bucket, Key: info.Key, ETag: info.ETag, Size: info.Size}, nil) {
				return
			}
		}
	}
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
