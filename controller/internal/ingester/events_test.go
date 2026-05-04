package ingester

import "testing"

func TestParseMinIONotificationFiltersObjectCreatedEvents(t *testing.T) {
	events, err := ParseMinIONotification([]byte(`{
		"Records": [
			{
				"eventName": "s3:ObjectCreated:Put",
				"s3": {
					"bucket": {"name": "dropcheck"},
					"object": {"key": "e2e/device%2Frun.pb", "eTag": "abc", "size": 42}
				}
			},
			{
				"eventName": "s3:ObjectRemoved:Delete",
				"s3": {
					"bucket": {"name": "dropcheck"},
					"object": {"key": "e2e/device/old.pb"}
				}
			}
		]
	}`))
	if err != nil {
		t.Fatalf("ParseMinIONotification: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1", len(events))
	}
	if events[0].Bucket != "dropcheck" || events[0].Key != "e2e/device/run.pb" || events[0].ETag != "abc" || events[0].Size != 42 {
		t.Fatalf("event = %#v", events[0])
	}
}
