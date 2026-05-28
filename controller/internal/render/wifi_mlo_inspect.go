package render

import (
	"encoding/hex"
	"fmt"
	"strings"

	"dropcheck/controller/internal/controlpb"
)

const (
	rnrElementID                         = 201
	multipleBSSIDElementID               = 71
	nontransmittedBSSIDProfileSubelement = 0
	nontransmittedBSSIDCapabilityID      = 83
	multipleBSSIDIndexID                 = 85
	nonInheritanceIDExt                  = 56
)

type wifiMLORNRNeighbor struct {
	headerRaw      int
	typ            int
	filtered       bool
	count          int
	infoLength     int
	operatingClass int
	channel        int
	tbtts          []wifiMLORNRtbtt
	truncated      bool
}

type wifiMLORNRtbtt struct {
	offset        int
	bssid         string
	shortSSID     string
	bssParameters *int
	psd20MHz      *int
	mld           *wifiMLORNRMldParameters
	rawHex        string
	truncated     bool
}

type wifiMLORNRMldParameters struct {
	apMLDID                  int
	linkID                   int
	bssParametersChangeCount int
	allUpdates               bool
	disabledLink             bool
}

type wifiMLOMultipleBSSID struct {
	maxBSSIDIndicator int
	profiles          []wifiMLOMultipleBSSIDProfile
	truncated         bool
}

type wifiMLOMultipleBSSIDProfile struct {
	index                   int
	declaredLength          int
	actualLength            int
	ssid                    string
	bssidIndex              *int
	dtimPeriod              *int
	bssidCapabilityHex      string
	nonInheritanceElementID []int
	nonInheritanceIDExt     []int
	elements                []ehtProfileInformationElement
	security                *controlpb.WifiSecurityDetails
	truncated               bool
	rawHex                  string
}

func renderWifiMLODeviceReadiness(b *strings.Builder, capabilities *controlpb.WifiCapabilities) {
	if capabilities == nil {
		return
	}
	rows := []kvRow{
		kv("band_6ghz", wifiMLOCapabilityState(capabilities.GetSupportedBands(), capabilities.GetUnsupportedBands(), "6GHz")),
		kv("standard_11be", wifiMLOCapabilityState(capabilities.GetSupportedStandards(), capabilities.GetUnsupportedStandards(), "802.11be")),
		kv("wpa3_sae", wifiMLOCapabilityState(capabilities.GetSupportedSecurityModes(), capabilities.GetUnsupportedSecurityModes(), "wpa3_sae")),
		kv("wpa3_sae_h2e", wifiMLOCapabilityState(capabilities.GetSupportedSecurityModes(), capabilities.GetUnsupportedSecurityModes(), "wpa3_sae_h2e")),
		kv("wpa3_sae_public_key", wifiMLOCapabilityState(capabilities.GetSupportedSecurityModes(), capabilities.GetUnsupportedSecurityModes(), "wpa3_sae_public_key")),
		kv("owe", wifiMLOCapabilityState(capabilities.GetSupportedSecurityModes(), capabilities.GetUnsupportedSecurityModes(), "owe")),
		kv("tid_to_link_mapping", wifiMLOCapabilityState(capabilities.GetSupportedFeatures(), capabilities.GetUnsupportedFeatures(), "tid_to_link_mapping_negotiation")),
		kv("dual_band_simultaneous", wifiMLOCapabilityState(capabilities.GetSupportedFeatures(), capabilities.GetUnsupportedFeatures(), "dual_band_simultaneous")),
		kv("sta_multi_internet", wifiMLOCapabilityState(capabilities.GetSupportedFeatures(), capabilities.GetUnsupportedFeatures(), "sta_concurrency_multi_internet")),
	}
	rows = nonEmptyKVRows(rows)
	if len(rows) == 0 {
		return
	}
	writeKVSection(b, "Wi-Fi 7 Device Readiness", rows...)
}

func wifiMLOCapabilityState(supported []string, unsupported []string, name string) string {
	if wifiMLOContainsValue(supported, name) {
		return "supported"
	}
	if wifiMLOContainsValue(unsupported, name) {
		return "unsupported"
	}
	return "unknown"
}

func wifiMLOInformationElementChecklist(result *controlpb.WifiScanResult) string {
	elements := result.GetInformationElements()
	return fmt.Sprintf(
		"ie rsn=%t rsnxe=%t ext_cap=%t rnr=%t mbssid=%t noninherit=%t eht_mle=%t ap_mld=%t link_id=%t",
		wifiMLOHasInformationElement(elements, 48, nil),
		wifiMLOHasInformationElement(elements, 244, nil),
		wifiMLOHasInformationElement(elements, 127, nil),
		wifiMLOHasInformationElement(elements, rnrElementID, nil),
		wifiMLOHasInformationElement(elements, multipleBSSIDElementID, nil),
		wifiMLOHasInformationElement(elements, 255, intPtr(nonInheritanceIDExt)),
		wifiMLOHasElement(elements),
		result.GetApMldMacAddress() != "" || wifiMLOMLDMACFromElements(elements) != "",
		result.GetApMloLinkId() >= 0 || wifiMLOCurrentLinkIDFromElements(elements) != nil,
	)
}

func wifiMLOScanSDKFlags(result *controlpb.WifiScanResult) string {
	return fmt.Sprintf(
		"sdk_flags twt=%t 11az_ntb=%t ranging_prot=%t secure_he_ltf=%t 11mc=%t",
		result.GetTwtResponder(),
		result.GetResponder_80211AzNtb(),
		result.GetRangingFrameProtectionRequired(),
		result.GetSecureHeLtfSupported(),
		result.GetResponder_80211Mc(),
	)
}

func wifiMLOHasInformationElement(elements []*controlpb.WifiInformationElement, id int32, idExt *int) bool {
	for _, element := range elements {
		if element.GetId() != id {
			continue
		}
		if idExt == nil || element.GetIdExt() == int32(*idExt) {
			return true
		}
	}
	return false
}

func renderWifiMLORNRDetails(b *strings.Builder, groups []wifiMLOGroup) {
	lines := []string{}
	for _, group := range groups {
		for _, result := range group.results {
			label := fmt.Sprintf("ap ssid=%s bssid=%s", empty(result.GetSsid(), "<hidden>"), empty(result.GetBssid(), "<unknown>"))
			lines = append(lines, formatWifiMLORNRDetails(label, result.GetInformationElements())...)
		}
	}
	if len(lines) == 0 {
		return
	}
	writeSection(b, "Scan RNR Details")
	for _, line := range lines {
		fmt.Fprintf(b, "  %s\n", line)
	}
}

func formatWifiMLORNRDetails(label string, elements []*controlpb.WifiInformationElement) []string {
	neighbors := parseWifiMLORNRNeighbors(elements)
	if len(neighbors) == 0 {
		return nil
	}
	lines := []string{label}
	for _, neighbor := range neighbors {
		truncated := ""
		if neighbor.truncated {
			truncated = " truncated=true"
		}
		lines = append(lines, fmt.Sprintf("  rnr op_class=%d channel=%d type=%d filtered=%t info_len=%d count=%d flags=%s raw=0x%04x%s",
			neighbor.operatingClass,
			neighbor.channel,
			neighbor.typ,
			neighbor.filtered,
			neighbor.infoLength,
			neighbor.count,
			wifiMLOJoinStrings(wifiMLORNRFieldFlags(neighbor.infoLength), "<none>"),
			neighbor.headerRaw,
			truncated,
		))
		for _, tbtt := range neighbor.tbtts {
			parts := []string{
				fmt.Sprintf("tbtt offset=%d", tbtt.offset),
			}
			if tbtt.bssid != "" {
				parts = append(parts, "bssid="+tbtt.bssid)
			}
			if tbtt.shortSSID != "" {
				parts = append(parts, "short_ssid=0x"+tbtt.shortSSID)
			}
			if tbtt.bssParameters != nil {
				parts = append(parts, fmt.Sprintf("bss_params=0x%02x", *tbtt.bssParameters))
			}
			if tbtt.psd20MHz != nil {
				parts = append(parts, fmt.Sprintf("psd20=%d", *tbtt.psd20MHz))
			}
			if tbtt.mld != nil {
				parts = append(parts, fmt.Sprintf("mld ap_mld_id=%d link_id=%d bss_change=%d all_updates=%t disabled=%t",
					tbtt.mld.apMLDID,
					tbtt.mld.linkID,
					tbtt.mld.bssParametersChangeCount,
					tbtt.mld.allUpdates,
					tbtt.mld.disabledLink,
				))
			}
			if tbtt.truncated {
				parts = append(parts, "truncated=true")
			}
			if tbtt.rawHex != "" {
				parts = append(parts, "raw=0x"+hexPreview(tbtt.rawHex, 24))
			}
			lines = append(lines, "  "+strings.Join(parts, " "))
		}
	}
	return lines
}

func parseWifiMLORNRNeighbors(elements []*controlpb.WifiInformationElement) []wifiMLORNRNeighbor {
	neighbors := []wifiMLORNRNeighbor{}
	for _, element := range elements {
		if element.GetId() != rnrElementID {
			continue
		}
		bytes, err := hex.DecodeString(element.GetBytesHex())
		if err != nil {
			continue
		}
		neighbors = append(neighbors, parseWifiMLORNRBytes(bytes)...)
	}
	return neighbors
}

func parseWifiMLORNRBytes(bytes []byte) []wifiMLORNRNeighbor {
	neighbors := []wifiMLORNRNeighbor{}
	offset := 0
	for offset < len(bytes) {
		if offset+4 > len(bytes) {
			if len(neighbors) > 0 {
				neighbors[len(neighbors)-1].truncated = true
			}
			break
		}
		header := u16le(bytes, offset)
		count := ((header >> 4) & 0x0f) + 1
		infoLength := (header >> 8) & 0xff
		neighbor := wifiMLORNRNeighbor{
			headerRaw:      header,
			typ:            header & 0x03,
			filtered:       header&0x04 != 0,
			count:          count,
			infoLength:     infoLength,
			operatingClass: int(bytes[offset+2]),
			channel:        int(bytes[offset+3]),
		}
		offset += 4
		if infoLength <= 0 {
			neighbor.truncated = true
			neighbors = append(neighbors, neighbor)
			break
		}
		for i := 0; i < count; i++ {
			if offset >= len(bytes) {
				neighbor.truncated = true
				break
			}
			end := min(offset+infoLength, len(bytes))
			tbtt := parseWifiMLORNRtbtt(bytes[offset:end], infoLength)
			neighbor.tbtts = append(neighbor.tbtts, tbtt)
			if end-offset != infoLength {
				neighbor.truncated = true
				break
			}
			offset = end
		}
		neighbors = append(neighbors, neighbor)
	}
	return neighbors
}

func parseWifiMLORNRtbtt(field []byte, declaredLength int) wifiMLORNRtbtt {
	tbtt := wifiMLORNRtbtt{
		rawHex:    hex.EncodeToString(field),
		truncated: len(field) != declaredLength,
	}
	if len(field) == 0 {
		tbtt.truncated = true
		return tbtt
	}
	tbtt.offset = int(field[0])
	if declaredLength >= 7 && len(field) >= 7 {
		tbtt.bssid = readMAC(field, 1, len(field))
	}
	if declaredLength >= 11 && len(field) >= 11 {
		tbtt.shortSSID = hex.EncodeToString(field[7:11])
	}
	if declaredLength >= 12 && len(field) >= 12 {
		value := int(field[11])
		tbtt.bssParameters = &value
	}
	if declaredLength >= 13 && len(field) >= 13 {
		value := int(field[12])
		tbtt.psd20MHz = &value
	}
	if declaredLength >= 16 && len(field) >= 16 {
		raw := u16le(field, 14)
		tbtt.mld = &wifiMLORNRMldParameters{
			apMLDID:                  int(field[13]),
			linkID:                   raw & 0x0f,
			bssParametersChangeCount: (raw >> 4) & 0xff,
			allUpdates:               raw&0x1000 != 0,
			disabledLink:             raw&0x2000 != 0,
		}
	}
	return tbtt
}

func wifiMLORNRFieldFlags(infoLength int) []string {
	flags := []string{"tbtt_offset"}
	if infoLength >= 7 {
		flags = append(flags, "bssid")
	}
	if infoLength >= 11 {
		flags = append(flags, "short_ssid")
	}
	if infoLength >= 12 {
		flags = append(flags, "bss_params")
	}
	if infoLength >= 13 {
		flags = append(flags, "psd20")
	}
	if infoLength >= 16 {
		flags = append(flags, "mld_params")
	}
	return flags
}

func renderWifiMLOMultipleBSSIDDetails(b *strings.Builder, groups []wifiMLOGroup) {
	lines := []string{}
	for _, group := range groups {
		for _, result := range group.results {
			label := fmt.Sprintf("ap ssid=%s bssid=%s", empty(result.GetSsid(), "<hidden>"), empty(result.GetBssid(), "<unknown>"))
			lines = append(lines, formatWifiMLOMultipleBSSIDDetails(label, result.GetInformationElements())...)
		}
	}
	if len(lines) == 0 {
		return
	}
	writeSection(b, "Scan Multiple BSSID Details")
	for _, line := range lines {
		fmt.Fprintf(b, "  %s\n", line)
	}
}

func formatWifiMLOMultipleBSSIDDetails(label string, elements []*controlpb.WifiInformationElement) []string {
	values := parseWifiMLOMultipleBSSID(elements)
	if len(values) == 0 {
		return nil
	}
	lines := []string{label}
	for _, value := range values {
		truncated := ""
		if value.truncated {
			truncated = " truncated=true"
		}
		lines = append(lines, fmt.Sprintf("  mbssid max_indicator=%d profiles=%d%s", value.maxBSSIDIndicator, len(value.profiles), truncated))
		for _, profile := range value.profiles {
			parts := []string{
				fmt.Sprintf("profile #%d", profile.index),
				fmt.Sprintf("len=%d", profile.declaredLength),
				fmt.Sprintf("actual=%d", profile.actualLength),
				"ssid=" + empty(profile.ssid, "<unknown>"),
			}
			if profile.bssidIndex != nil {
				parts = append(parts, fmt.Sprintf("bssid_index=%d", *profile.bssidIndex))
			}
			if profile.dtimPeriod != nil {
				parts = append(parts, fmt.Sprintf("dtim_period=%d", *profile.dtimPeriod))
			}
			if profile.bssidCapabilityHex != "" {
				parts = append(parts, "cap=0x"+profile.bssidCapabilityHex)
			}
			if len(profile.nonInheritanceElementID) > 0 || len(profile.nonInheritanceIDExt) > 0 {
				parts = append(parts, fmt.Sprintf("noninherit=ids:%s/ext:%s",
					wifiMLOJoinIntsFromInt(profile.nonInheritanceElementID, "<none>"),
					wifiMLOJoinIntsFromInt(profile.nonInheritanceIDExt, "<none>"),
				))
			}
			if profile.truncated {
				parts = append(parts, "truncated=true")
			}
			parts = append(parts, "ies="+wifiMLOProfileElementNames(profile.elements))
			lines = append(lines, "  "+strings.Join(parts, " "))
			if profile.security != nil {
				lines = append(lines, fmt.Sprintf("  profile_security #%d", profile.index))
				for _, line := range wifiSecuritySummaryLines(profile.security) {
					lines = append(lines, "    "+line)
				}
			}
		}
	}
	return lines
}

func parseWifiMLOMultipleBSSID(elements []*controlpb.WifiInformationElement) []wifiMLOMultipleBSSID {
	values := []wifiMLOMultipleBSSID{}
	for _, element := range elements {
		if element.GetId() != multipleBSSIDElementID {
			continue
		}
		bytes, err := hex.DecodeString(element.GetBytesHex())
		if err != nil || len(bytes) == 0 {
			continue
		}
		values = append(values, parseWifiMLOMultipleBSSIDBytes(bytes))
	}
	return values
}

func parseWifiMLOMultipleBSSIDBytes(bytes []byte) wifiMLOMultipleBSSID {
	value := wifiMLOMultipleBSSID{maxBSSIDIndicator: int(bytes[0])}
	offset := 1
	profileIndex := 0
	for offset < len(bytes) {
		if offset+2 > len(bytes) {
			value.truncated = true
			break
		}
		id := int(bytes[offset])
		declaredLength := int(bytes[offset+1])
		dataStart := offset + 2
		dataEnd := min(dataStart+declaredLength, len(bytes))
		data := bytes[dataStart:dataEnd]
		truncated := len(data) != declaredLength
		if id == nontransmittedBSSIDProfileSubelement {
			profileIndex++
			value.profiles = append(value.profiles, parseWifiMLOMultipleBSSIDProfile(profileIndex, declaredLength, data, truncated))
		}
		value.truncated = value.truncated || truncated
		if truncated {
			break
		}
		offset = dataEnd
	}
	return value
}

func parseWifiMLOMultipleBSSIDProfile(index int, declaredLength int, data []byte, truncated bool) wifiMLOMultipleBSSIDProfile {
	elements, elementsTruncated := parseProfileInformationElements(data)
	profile := wifiMLOMultipleBSSIDProfile{
		index:          index,
		declaredLength: declaredLength,
		actualLength:   len(data),
		elements:       elements,
		truncated:      truncated || elementsTruncated,
		rawHex:         hex.EncodeToString(data),
		security:       wifiSecurityDetailsFromProfileElements(elements),
	}
	for _, element := range elements {
		switch element.id {
		case 0:
			if ssid, err := hex.DecodeString(element.bodyHex); err == nil {
				profile.ssid = string(ssid)
			}
		case multipleBSSIDIndexID:
			if body, err := hex.DecodeString(element.bodyHex); err == nil && len(body) > 0 {
				index := int(body[0])
				profile.bssidIndex = &index
				if len(body) > 1 {
					dtim := int(body[1])
					profile.dtimPeriod = &dtim
				}
			}
		case nontransmittedBSSIDCapabilityID:
			if body, err := hex.DecodeString(element.bodyHex); err == nil && len(body) >= 2 {
				profile.bssidCapabilityHex = hex.EncodeToString(body[:2])
			}
		case ehtMultiLinkElementID:
			if element.idExt != nil && *element.idExt == nonInheritanceIDExt {
				profile.nonInheritanceElementID, profile.nonInheritanceIDExt = parseWifiMLONonInheritance(element.bodyHex)
			}
		}
	}
	return profile
}

func parseWifiMLONonInheritance(bodyHex string) ([]int, []int) {
	body, err := hex.DecodeString(bodyHex)
	if err != nil || len(body) == 0 {
		return nil, nil
	}
	offset := 1
	if offset >= len(body) {
		return nil, nil
	}
	idCount := int(body[offset])
	offset++
	ids := []int{}
	for i := 0; i < idCount && offset < len(body); i++ {
		ids = append(ids, int(body[offset]))
		offset++
	}
	if offset >= len(body) {
		return ids, nil
	}
	extCount := int(body[offset])
	offset++
	exts := []int{}
	for i := 0; i < extCount && offset < len(body); i++ {
		exts = append(exts, int(body[offset]))
		offset++
	}
	return ids, exts
}

func wifiMLOProfileElementNames(elements []ehtProfileInformationElement) string {
	names := []string{}
	for _, element := range elements {
		names = append(names, element.name)
	}
	return wifiMLOJoinStrings(names, "<none>")
}

func wifiSecurityDetailsFromProfileElements(elements []ehtProfileInformationElement) *controlpb.WifiSecurityDetails {
	var rsn []byte
	var rsnxe []byte
	var extended []byte
	for _, element := range elements {
		body, err := hex.DecodeString(element.bodyHex)
		if err != nil {
			continue
		}
		switch element.id {
		case 48:
			rsn = body
		case 244:
			rsnxe = body
		case 127:
			extended = body
		}
	}
	if rsn == nil && rsnxe == nil && extended == nil {
		return nil
	}
	return parseWifiSecurityDetailsFromBytes(rsn, rsnxe, extended)
}

func parseWifiSecurityDetailsFromBytes(rsn []byte, rsnxe []byte, extended []byte) *controlpb.WifiSecurityDetails {
	builder := &controlpb.WifiSecurityDetails{}
	if rsn != nil {
		parseWifiRSNBytes(rsn, builder)
	}
	if rsnxe != nil {
		builder.RsnxePresent = true
		builder.RawRsnxeHex = hex.EncodeToString(rsnxe)
		builder.RsnxeCapabilities = wifiRSNXECapabilityNames(rsnxe)
	}
	if extended != nil {
		builder.ExtendedCapabilitiesPresent = true
		builder.RawExtendedCapabilitiesHex = hex.EncodeToString(extended)
		builder.ExtendedCapabilities = wifiExtendedCapabilityNames(extended)
		builder.BeaconProtection = wifiInformationElementBit(extended, 84)
	}
	builder.Gcmp_256 = builder.GroupDataCipher == "gcmp_256" || slicesContains(builder.PairwiseCiphers, "gcmp_256")
	builder.SaeGdh = slicesContains(builder.AkmSuites, "sae_gdh")
	builder.FtSaeGdh = slicesContains(builder.AkmSuites, "ft_sae_gdh")
	builder.Wifi7PersonalReady = builder.PmfRequired && builder.Gcmp_256 && (builder.SaeGdh || builder.FtSaeGdh) && builder.BeaconProtection
	return builder
}

func parseWifiRSNBytes(bytes []byte, value *controlpb.WifiSecurityDetails) {
	value.RsnPresent = true
	value.RawRsnHex = hex.EncodeToString(bytes)
	offset := 0
	require := func(length int, field string) bool {
		if len(bytes) >= offset+length {
			return true
		}
		value.Warnings = append(value.Warnings, fmt.Sprintf("rsn_%s_too_short bytes=%d required=%d", field, max(len(bytes)-offset, 0), length))
		return false
	}
	if !require(2, "version") {
		return
	}
	value.RsnVersion = uint32(u16le(bytes, offset))
	offset += 2
	if !require(4, "group_data_cipher") {
		return
	}
	value.GroupDataCipher = wifiRSNCipherSuiteName(bytes, offset)
	offset += 4
	if !require(2, "pairwise_cipher_count") {
		return
	}
	pairwiseCount := u16le(bytes, offset)
	offset += 2
	for i := 0; i < pairwiseCount; i++ {
		if !require(4, fmt.Sprintf("pairwise_cipher_%d", i)) {
			return
		}
		value.PairwiseCiphers = append(value.PairwiseCiphers, wifiRSNCipherSuiteName(bytes, offset))
		offset += 4
	}
	if !require(2, "akm_count") {
		return
	}
	akmCount := u16le(bytes, offset)
	offset += 2
	for i := 0; i < akmCount; i++ {
		if !require(4, fmt.Sprintf("akm_suite_%d", i)) {
			return
		}
		value.AkmSuites = append(value.AkmSuites, wifiRSNAKMSuiteName(bytes, offset))
		offset += 4
	}
	if len(bytes) < offset+2 {
		return
	}
	capabilities := u16le(bytes, offset)
	value.RsnCapabilities = uint32(capabilities)
	value.RsnCapabilitiesHex = fmt.Sprintf("%04x", capabilities&0xffff)
	value.PmfRequired = capabilities&(1<<6) != 0
	value.PmfCapable = capabilities&(1<<7) != 0
	offset += 2
	if len(bytes) < offset+2 {
		return
	}
	pmkidCount := u16le(bytes, offset)
	offset += 2
	pmkidBytes := pmkidCount * 16
	if len(bytes) < offset+pmkidBytes {
		value.Warnings = append(value.Warnings, fmt.Sprintf("rsn_pmkid_list_too_short bytes=%d required=%d", max(len(bytes)-offset, 0), pmkidBytes))
		return
	}
	offset += pmkidBytes
	if len(bytes) >= offset+4 {
		value.GroupManagementCipher = wifiRSNCipherSuiteName(bytes, offset)
	} else if len(bytes) > offset {
		value.Warnings = append(value.Warnings, fmt.Sprintf("rsn_group_management_cipher_too_short bytes=%d required=4", len(bytes)-offset))
	}
}

func wifiRSNCipherSuiteName(bytes []byte, offset int) string {
	if offset+4 > len(bytes) {
		return "<truncated>"
	}
	oui := hex.EncodeToString(bytes[offset : offset+3])
	typ := int(bytes[offset+3])
	if oui != "000fac" {
		return fmt.Sprintf("cipher_%s_%d", oui, typ)
	}
	switch typ {
	case 0:
		return "use_group"
	case 1:
		return "wep40"
	case 2:
		return "tkip"
	case 4:
		return "ccmp_128"
	case 5:
		return "wep104"
	case 6:
		return "bip_cmac_128"
	case 7:
		return "no_group_addressed"
	case 8:
		return "gcmp_128"
	case 9:
		return "gcmp_256"
	case 10:
		return "ccmp_256"
	case 11:
		return "bip_gmac_128"
	case 12:
		return "bip_gmac_256"
	case 13:
		return "bip_cmac_256"
	default:
		return fmt.Sprintf("rsn_cipher_000fac_%d", typ)
	}
}

func wifiRSNAKMSuiteName(bytes []byte, offset int) string {
	if offset+4 > len(bytes) {
		return "<truncated>"
	}
	oui := hex.EncodeToString(bytes[offset : offset+3])
	typ := int(bytes[offset+3])
	if oui != "000fac" {
		return fmt.Sprintf("akm_%s_%d", oui, typ)
	}
	switch typ {
	case 1:
		return "8021x"
	case 2:
		return "psk"
	case 3:
		return "ft_8021x"
	case 4:
		return "ft_psk"
	case 5:
		return "8021x_sha256"
	case 6:
		return "psk_sha256"
	case 7:
		return "tdls"
	case 8:
		return "sae"
	case 9:
		return "ft_sae"
	case 11:
		return "8021x_suite_b"
	case 12:
		return "8021x_suite_b_192"
	case 18:
		return "owe"
	case 24:
		return "sae_gdh"
	case 25:
		return "ft_sae_gdh"
	default:
		return fmt.Sprintf("rsn_akm_000fac_%d", typ)
	}
}

func wifiRSNXECapabilityNames(bytes []byte) []string {
	names := []string{}
	for _, entry := range []struct {
		bit  int
		name string
	}{
		{4, "protected_twt"},
		{5, "sae_h2e"},
		{6, "sae_pk"},
		{8, "secure_ltf"},
		{9, "secure_rtt"},
		{10, "urnm_mfpr_x20"},
		{14, "spp_a_msdu"},
		{15, "urnm_mfpr"},
		{18, "kek_in_pasn"},
		{21, "ssid_protection"},
		{27, "assoc_frame_encryption"},
		{28, "8021x_in_auth_frames"},
		{29, "pmksa_caching_privacy"},
		{34, "sae_pw_id_change"},
		{36, "unauth_eppke"},
	} {
		if wifiInformationElementBit(bytes, entry.bit) {
			names = append(names, entry.name)
		}
	}
	return names
}

func wifiExtendedCapabilityNames(bytes []byte) []string {
	names := []string{}
	for _, entry := range []struct {
		bit  int
		name string
	}{
		{19, "bss_transition"},
		{72, "fils"},
		{80, "complete_non_tx_bssid_profile"},
		{81, "sae_password_identifier"},
		{82, "sae_password_identifier_exclusively"},
		{84, "beacon_protection"},
		{88, "sae_pk_exclusively"},
		{102, "known_sta_identification"},
	} {
		if wifiInformationElementBit(bytes, entry.bit) {
			names = append(names, entry.name)
		}
	}
	return names
}

func wifiInformationElementBit(bytes []byte, bit int) bool {
	if bit < 0 {
		return false
	}
	index := bit / 8
	if index >= len(bytes) {
		return false
	}
	return int(bytes[index])&(1<<(bit%8)) != 0
}

func wifiMLOJoinIntsFromInt(values []int, emptyValue string) string {
	if len(values) == 0 {
		return emptyValue
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, fmt.Sprint(value))
	}
	return strings.Join(parts, ",")
}

func slicesContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func intPtr(value int) *int {
	return &value
}
