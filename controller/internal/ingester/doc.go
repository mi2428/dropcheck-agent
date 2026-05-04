// Package ingester reads standalone Dropcheck result archives from MinIO and
// publishes low-cardinality Prometheus gauges through Pushgateway.
//
// The package treats one festa/device/Wi-Fi target as the stable Pushgateway
// grouping key. Upload identifiers such as run_id and object_key are parsed for
// logs and deduplication only; they are intentionally not part of Prometheus
// labels because each upload would otherwise create a new time series.
package ingester
