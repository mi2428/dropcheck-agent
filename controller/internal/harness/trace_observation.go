package harness

import "dropcheck/controller/internal/controlpb"

// TraceObservations is the shared presence/count rule for typed and YAML
// expectations. Timed-out TTL records are observations and count as hops.
func TraceObservations(raw *controlpb.TracerouteResult) (int, []string, bool, string) {
	if raw == nil || raw.ReachedTarget == nil {
		return 0, nil, false, "hop observation unavailable"
	}
	if raw.ObservationError != "" {
		return 0, nil, false, raw.ObservationError
	}
	var addresses []string
	for _, hop := range raw.Hops {
		addresses = append(addresses, hop.GetAddresses()...)
	}
	return len(raw.Hops), uniqueStrings(addresses), true, ""
}
