package ingester

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/minio/minio-go/v7"
)

func TestMinIOListingStreamsPagesAndStopsEarly(t *testing.T) {
	for _, stopEarly := range []bool{false, true} {
		t.Run(fmt.Sprintf("stop_early=%v", stopEarly), func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "application/xml")
				if r.URL.Query().Get("continuation-token") == "next" {
					_, _ = fmt.Fprint(w, `<ListBucketResult><IsTruncated>false</IsTruncated><Contents><Key>incoming/second.pb</Key><ETag>second</ETag><Size>1</Size></Contents></ListBucketResult>`)
					return
				}
				_, _ = fmt.Fprint(w, `<ListBucketResult><IsTruncated>true</IsTruncated><NextContinuationToken>next</NextContinuationToken><Contents><Key>incoming/ignored.txt</Key><ETag>ignored</ETag><Size>1</Size></Contents><Contents><Key>incoming/first.pb</Key><ETag>first</ETag><Size>1</Size></Contents></ListBucketResult>`)
			}))
			defer server.Close()
			client, err := minio.New(strings.TrimPrefix(server.URL, "http://"), &minio.Options{Region: "us-east-1"})
			if err != nil {
				t.Fatal(err)
			}
			store := &MinIOStore{client: client, bucket: "synthetic", prefix: "incoming", suffix: ".pb"}
			count := 0
			for object, err := range store.ListObjects(context.Background()) {
				if err != nil {
					t.Fatal(err)
				}
				if object.Bucket != "synthetic" || !strings.HasSuffix(object.Key, ".pb") {
					t.Fatalf("unexpected listed object: %+v", object)
				}
				count++
				if stopEarly {
					break
				}
			}
			want := 2
			if stopEarly {
				want = 1
			}
			if count != want {
				t.Fatalf("objects=%d, want %d", count, want)
			}
			// The SDK may prefetch the next page, but early exit must not wait for
			// or drain the remaining enumeration.
			if requests.Load() > 2 {
				t.Fatalf("unexpected listing requests: %d", requests.Load())
			}
		})
	}
}
