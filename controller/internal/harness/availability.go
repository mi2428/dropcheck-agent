package harness

import "dropcheck/controller/internal/controlpb"

// Availability consumes collector metadata. Empty lists and scalar zero cannot
// establish availability; absent metadata is explicitly unknown.
func Availability(fields []*controlpb.DiagnosticField, field string) (bool, string) {
	state, reason := "", "availability not reported"
	for _, f := range fields {
		if f.GetKey() == field+".state" {
			state = f.GetValue()
		}
		if f.GetKey() == field+".reason" {
			reason = f.GetValue()
		}
	}
	return state == "available", reason
}
