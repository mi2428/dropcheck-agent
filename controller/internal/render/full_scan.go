package render

import (
	"fmt"
	"strings"

	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
)

func renderInformationElements(b *strings.Builder, elements []*controlpb.WifiInformationElement) {
	if len(elements) > 0 {
		writeSection(b, "Information Elements")
	}
	var rows strings.Builder
	for i, element := range elements {
		writeRecord(&rows, fmt.Sprintf("IE %d", i+1), fmt.Sprintf("id=%d extension=%d bytes=%d hex=%s", element.GetId(), element.GetIdExt(), element.GetByteCount(), element.GetBytesHex()))
	}
	writeNested(b, rows.String())
}

// Shared decoded capability renderers receive the exact AP's evidence, not a new observation.
func renderFullScanEvidence(b *strings.Builder, ap *controlpb.WifiScanResult) {
	writeSection(b, "AP properties")
	writeRecord(b, "Operator", ap.GetOperatorFriendlyName())
	writeRecord(b, "Venue", ap.GetVenueName())
	writeRecord(b, "AP link ID", observedValue(ap.GetObservationFields(), "ap_mlo_link_id", fmt.Sprint(ap.GetApMloLinkId())))
	conn := &controlpb.WifiConnection{InformationElements: ap.GetInformationElements(), HeCapabilities: ap.GetHeCapabilities(), HeOperation: ap.GetHeOperation(), EhtCapabilities: ap.GetEhtCapabilities(), EhtOperation: ap.GetEhtOperation(), HeUoraParameterSet: ap.GetHeUoraParameterSet(), HeMuEdcaParameterSet: ap.GetHeMuEdcaParameterSet(), HeSpatialReuseParameterSet: ap.GetHeSpatialReuseParameterSet(), He_6GhzCapabilities: ap.GetHe_6GhzCapabilities()}
	renderWifiConnectionDetailedCapabilities(b, conn)
	renderScanReferenceEvidence(b, ap)
}

func renderScanReferenceEvidence(b *strings.Builder, ap *controlpb.WifiScanResult) {
	writeSection(b, "Scan properties")
	writeRecord(b, "Capabilities", ap.GetCapabilities())
	writeRecord(b, "WifiSsid representation", ap.GetWifiSsid())
	writeRecord(b, "Timestamp us (boot clock)", ap.GetTimestampUs())
	writeRecord(b, "Passpoint", ap.GetPasspoint())
	renderInformationElements(b, ap.GetInformationElements())
	renderDiagnosticFields(b, ap.GetObservationFields())
}
