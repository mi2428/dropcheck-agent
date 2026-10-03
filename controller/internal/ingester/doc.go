// Package ingester reads standalone Dropcheck result archives from MinIO and
// publishes low-cardinality Prometheus gauges through Pushgateway.
//
// The package treats one festa/device/Wi-Fi target as the stable Pushgateway
// grouping key. Upload identifiers such as run_id and object_key are parsed for
// logs and deduplication only; they are intentionally not part of Prometheus
// labels because each upload would otherwise create a new time series.
// Replacement order uses a positive summary finished_unix_ms, otherwise a
// positive started_unix_ms. Equal timestamps prefer the lexically greater object
// key; changed signatures of the same key can still update and retry.
// Undated historical archives retain arrival-order updates only until a dated
// measurement succeeds for that group. They cannot overwrite a known timestamp.
// Fences advance per successful group push and are rebuilt by backfill after
// restart, without prefetching or sorting archives. They are not persisted, so a
// restart scan may temporarily push older values before reaching the newest.
package ingester
