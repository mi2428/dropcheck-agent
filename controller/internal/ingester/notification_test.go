package ingester

import (
	"strings"
	"testing"
)

func TestDecodeNotificationExtractsCreatedProtobufObjects(t *testing.T) {
	body := `{
	  "Records": [
	    {"eventName": "s3:ObjectCreated:Put", "s3": {"bucket": {"name": "dropcheck"}, "object": {"key": "device%2Frun-1.pb", "eTag": "abc", "size": 12}}},
	    {"eventName": "s3:ObjectCreated:Put", "s3": {"bucket": {"name": "dropcheck"}, "object": {"key": "device%2Frun-2.txt", "size": 2}}},
	    {"eventName": "s3:ObjectRemoved:Delete", "s3": {"bucket": {"name": "dropcheck"}, "object": {"key": "device%2Frun-3.pb", "size": 3}}}
	  ]
	}`
	objects, err := DecodeNotification(strings.NewReader(body), ".pb")
	if err != nil {
		t.Fatalf("DecodeNotification: %v", err)
	}
	if len(objects) != 1 {
		t.Fatalf("objects len = %d, want 1: %#v", len(objects), objects)
	}
	if objects[0].Bucket != "dropcheck" || objects[0].Key != "device/run-1.pb" || objects[0].ETag != "abc" || objects[0].Size != 12 {
		t.Fatalf("object = %#v", objects[0])
	}
}
