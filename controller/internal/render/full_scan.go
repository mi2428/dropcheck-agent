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
	var properties strings.Builder
	writeRecord(&properties, "Operator", ap.GetOperatorFriendlyName())
	writeRecord(&properties, "Venue", ap.GetVenueName())
	writeRecord(&properties, "AP link ID", observedValue(ap.GetObservationFields(), "ap_mlo_link_id", fmt.Sprint(ap.GetApMloLinkId())))
	writeNested(b, properties.String())
	conn := &controlpb.WifiConnection{InformationElements: ap.GetInformationElements(), HeCapabilities: ap.GetHeCapabilities(), HeOperation: ap.GetHeOperation(), EhtCapabilities: ap.GetEhtCapabilities(), EhtOperation: ap.GetEhtOperation(), HeUoraParameterSet: ap.GetHeUoraParameterSet(), HeMuEdcaParameterSet: ap.GetHeMuEdcaParameterSet(), HeSpatialReuseParameterSet: ap.GetHeSpatialReuseParameterSet(), He_6GhzCapabilities: ap.GetHe_6GhzCapabilities()}
	renderWifiConnectionDetailedCapabilities(b, conn)
	renderScanReferenceEvidence(b, ap)
}

func renderScanReferenceEvidence(b *strings.Builder, ap *controlpb.WifiScanResult) {
	writeSection(b, "Scan properties")
	var properties strings.Builder
	writeRecord(&properties, "Capabilities", ap.GetCapabilities())
	writeRecord(&properties, "WifiSsid representation", ap.GetWifiSsid())
	writeRecord(&properties, "Timestamp us (boot clock)", ap.GetTimestampUs())
	writeRecord(&properties, "Passpoint", ap.GetPasspoint())
	writeNested(b, properties.String())
	renderInformationElements(b, ap.GetInformationElements())
	renderDiagnosticFields(b, ap.GetObservationFields())
}
