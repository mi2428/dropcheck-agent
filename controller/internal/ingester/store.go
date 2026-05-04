package ingester

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// ObjectMeta carries enough object identity for dedupe and metric labels.
type ObjectMeta struct {
	Bucket       string
	Key          string
	ETag         string
	LastModified time.Time
	Size         int64
}

func (m ObjectMeta) Signature() string {
	return fmt.Sprintf("%s/%s:%s:%d:%d", m.Bucket, m.Key, m.ETag, m.LastModified.UnixNano(), m.Size)
}

func (m ObjectMeta) ObjectHash() string {
	sum := sha256.Sum256([]byte(m.Bucket + "\x00" + m.Key))
	return hex.EncodeToString(sum[:8])
}

type objectStore interface {
	Accepts(key string) bool
	List(ctx context.Context) ([]ObjectMeta, error)
	Read(ctx context.Context, key string) ([]byte, ObjectMeta, error)
}

type minIOStore struct {
	client   *minio.Client
	bucket   string
	prefix   string
	maxBytes int64
}

func newMinIOStore(cfg Config) (*minIOStore, error) {
	client, err := minio.New(cfg.MinIOEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.MinIOAccessKey, cfg.MinIOSecretKey, ""),
		Secure: cfg.MinIOSecure,
	})
	if err != nil {
		return nil, fmt.Errorf("create minio client: %w", err)
	}
	return &minIOStore{
		client:   client,
		bucket:   cfg.MinIOBucket,
		prefix:   cfg.MinIOPrefix,
		maxBytes: cfg.MaxObjectBytes,
	}, nil
}

func (s *minIOStore) Accepts(key string) bool {
	if !strings.HasSuffix(key, ".pb") {
		return false
	}
	return s.prefix == "" || strings.HasPrefix(key, s.prefix+"/") || key == s.prefix
}

func (s *minIOStore) List(ctx context.Context) ([]ObjectMeta, error) {
	options := minio.ListObjectsOptions{Prefix: s.prefix, Recursive: true}
	objects := s.client.ListObjects(ctx, s.bucket, options)
	var metas []ObjectMeta
	for object := range objects {
		if object.Err != nil {
			return nil, fmt.Errorf("list minio objects: %w", object.Err)
		}
		if !s.Accepts(object.Key) {
			continue
		}
		metas = append(metas, ObjectMeta{
			Bucket:       s.bucket,
			Key:          object.Key,
			ETag:         object.ETag,
			LastModified: object.LastModified,
			Size:         object.Size,
		})
	}
	return metas, nil
}

func (s *minIOStore) Read(ctx context.Context, key string) ([]byte, ObjectMeta, error) {
	meta := ObjectMeta{Bucket: s.bucket, Key: key}
	if !s.Accepts(key) {
		return nil, meta, fmt.Errorf("object %q does not match configured prefix/suffix", key)
	}
	stat, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return nil, meta, fmt.Errorf("stat minio object %q: %w", key, err)
	}
	meta.ETag = stat.ETag
	meta.LastModified = stat.LastModified
	meta.Size = stat.Size
	if stat.Size > s.maxBytes {
		return nil, meta, fmt.Errorf("object %q is %d bytes, max is %d", key, stat.Size, s.maxBytes)
	}
	object, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, meta, fmt.Errorf("get minio object %q: %w", key, err)
	}
	defer object.Close()
	data, err := io.ReadAll(io.LimitReader(object, s.maxBytes+1))
	if err != nil {
		return nil, meta, fmt.Errorf("read minio object %q: %w", key, err)
	}
	if int64(len(data)) > s.maxBytes {
		return nil, meta, fmt.Errorf("object %q exceeded max read size %d", key, s.maxBytes)
	}
	return data, meta, nil
}
