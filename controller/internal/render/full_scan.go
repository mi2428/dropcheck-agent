package render

import (
	"fmt"
	"strings"

	"dropcheck/controller/internal/controlpb"
)

func renderInformationElements(b *strings.Builder, elements []*controlpb.WifiInformationElement) {
	for i, element := range elements {
		writeRecord(b, fmt.Sprintf("IE %d", i+1), fmt.Sprintf("id=%d extension=%d bytes=%d hex=%s", element.GetId(), element.GetIdExt(), element.GetByteCount(), element.GetBytesHex()))
	}
}

// Shared decoded capability renderers receive the exact AP's evidence, not a new observation.
func renderFullScanEvidence(b *strings.Builder, ap *controlpb.WifiScanResult) {
	writeRecord(b, "Capabilities", ap.GetCapabilities())
	writeRecord(b, "WifiSsid representation", ap.GetWifiSsid())
	writeRecord(b, "Timestamp us (boot clock)", ap.GetTimestampUs())
	writeRecord(b, "Operator", ap.GetOperatorFriendlyName())
	writeRecord(b, "Venue", ap.GetVenueName())
	writeRecord(b, "Passpoint", ap.GetPasspoint())
	writeRecord(b, "AP link ID", observedValue(ap.GetObservationFields(), "ap_mlo_link_id", fmt.Sprint(ap.GetApMloLinkId())))
	conn := &controlpb.WifiConnection{InformationElements: ap.GetInformationElements(), HeCapabilities: ap.GetHeCapabilities(), HeOperation: ap.GetHeOperation(), EhtCapabilities: ap.GetEhtCapabilities(), EhtOperation: ap.GetEhtOperation(), HeUoraParameterSet: ap.GetHeUoraParameterSet(), HeMuEdcaParameterSet: ap.GetHeMuEdcaParameterSet(), HeSpatialReuseParameterSet: ap.GetHeSpatialReuseParameterSet(), He_6GhzCapabilities: ap.GetHe_6GhzCapabilities()}
	renderWifiConnectionDetailedCapabilities(b, conn)
	renderInformationElements(b, ap.GetInformationElements())
	if ap.GetRaw() != "" {
		writeRecord(b, "Raw ScanResult reference", ap.GetRaw())
	}
	renderDiagnosticFields(b, ap.GetObservationFields())
}
