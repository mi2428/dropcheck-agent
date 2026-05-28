package render

import (
	"encoding/hex"
	"fmt"
	"strings"

	"dropcheck/controller/internal/controlpb"
)

const (
	ehtMultiLinkElementID            = 255
	ehtMultiLinkIDExt                = 107
	multiLinkTypeBasic               = 0
	perSTAProfileSubelementID        = 0
	legacyFragmentSubelementID       = 1
	legacyVendorSpecificSubelementID = 2
	vendorSpecificSubelementID       = 221
	fragmentSubelementID             = 254
)

type ehtMultiLinkElement struct {
	control      int
	typ          int
	typeName     string
	presence     []string
	commonInfo   *ehtMultiLinkCommonInfo
	subelements  []ehtMultiLinkSubelement
	rawByteCount int
	truncated    bool
}

type ehtMultiLinkCommonInfo struct {
	length                            int
	mldMACAddress                     string
	linkID                            *int
	bssParametersChangeCount          *int
	mediumSynchronizationDelayInfoHex string
	emlCapabilitiesHex                string
	mldCapabilitiesAndOperationsHex   string
	apMLDID                           *int
}

type ehtMultiLinkSubelement struct {
	id             int
	name           string
	declaredLength int
	actualLength   int
	perSTAProfile  *ehtPerSTAProfile
	vendorSpecific *ehtVendorSpecificSubelement
	fragment       *ehtFragmentSubelement
	fragmentCount  int
	reassembledLen *int
	rawHex         string
	truncated      bool
}

type ehtVendorSpecificSubelement struct {
	oui              string
	vendorType       *int
	payloadByteCount int
	payloadHex       string
}

type ehtFragmentSubelement struct {
	targetSubelementID   *int
	targetSubelementName string
	byteCount            int
	rawHex               string
}

type ehtPerSTAProfile struct {
	control                  int
	linkID                   int
	completeProfile          bool
	flags                    []string
	staInfoLength            *int
	staMACAddress            string
	beaconIntervalTU         *int
	tsfOffsetHex             string
	dtimCount                *int
	dtimPeriod               *int
	nstrLinkPairHex          string
	bssParametersChangeCount *int
	profileByteCount         int
	profileHex               string
	profileElements          []ehtProfileInformationElement
	profileElementsTruncated bool
	truncated                bool
}

type ehtProfileInformationElement struct {
	id             int
	idExt          *int
	name           string
	declaredLength int
	actualLength   int
	bodyHex        string
	truncated      bool
}

func parseEHTMultiLinkElements(elements []*controlpb.WifiInformationElement) []ehtMultiLinkElement {
	parsed := make([]ehtMultiLinkElement, 0)
	for _, element := range elements {
		if element.GetId() != ehtMultiLinkElementID || element.GetIdExt() != ehtMultiLinkIDExt {
			continue
		}
		bytes, err := hex.DecodeString(element.GetBytesHex())
		if err != nil {
			continue
		}
		if value, ok := parseEHTMultiLinkElement(bytes, int(element.GetByteCount())); ok {
			parsed = append(parsed, value)
		}
	}
	return parsed
}

func formatEHTMultiLinkElements(label string, elements []ehtMultiLinkElement) []string {
	if len(elements) == 0 {
		return nil
	}
	lines := []string{}
	for i, element := range elements {
		suffix := ""
		if len(elements) > 1 {
			suffix = fmt.Sprintf(" #%d", i+1)
		}
		truncated := ""
		if element.truncated {
			truncated = " truncated=true"
		}
		lines = append(lines,
			fmt.Sprintf("%s%s", label, suffix),
			fmt.Sprintf("  ml_control raw=0x%04x type=%s(%d) presence=%s bytes=%d%s", element.control, element.typeName, element.typ, joinedOrNone(element.presence), element.rawByteCount, truncated),
		)
		if common := element.commonInfo; common != nil {
			parts := []string{fmt.Sprintf("common_info len=%d", common.length)}
			if common.mldMACAddress != "" {
				parts = append(parts, "mld_mac="+common.mldMACAddress)
			}
			if common.linkID != nil {
				parts = append(parts, fmt.Sprintf("link_id=%d", *common.linkID))
			}
			if common.bssParametersChangeCount != nil {
				parts = append(parts, fmt.Sprintf("bss_param_change_count=%d", *common.bssParametersChangeCount))
			}
			if common.mediumSynchronizationDelayInfoHex != "" {
				parts = append(parts, "medium_sync_delay=0x"+common.mediumSynchronizationDelayInfoHex)
			}
			if common.emlCapabilitiesHex != "" {
				parts = append(parts, "eml_capabilities=0x"+common.emlCapabilitiesHex)
			}
			if common.mldCapabilitiesAndOperationsHex != "" {
				parts = append(parts, "mld_capabilities=0x"+common.mldCapabilitiesAndOperationsHex)
			}
			if common.apMLDID != nil {
				parts = append(parts, fmt.Sprintf("ap_mld_id=%d", *common.apMLDID))
			}
			lines = append(lines, "  "+strings.Join(parts, " "))
		}
		for _, subelement := range element.subelements {
			truncated := ""
			if subelement.truncated {
				truncated = " truncated=true"
			}
			assembly := ""
			if subelement.fragmentCount > 0 {
				assembly = fmt.Sprintf(" fragments=%d", subelement.fragmentCount)
				if subelement.reassembledLen != nil {
					assembly += fmt.Sprintf(" reassembled=%d", *subelement.reassembledLen)
				}
			}
			lines = append(lines, fmt.Sprintf("  subelement id=%d name=%s len=%d actual=%d%s%s", subelement.id, subelement.name, subelement.declaredLength, subelement.actualLength, assembly, truncated))
			if fragment := subelement.fragment; fragment != nil {
				targetID := "?"
				if fragment.targetSubelementID != nil {
					targetID = fmt.Sprint(*fragment.targetSubelementID)
				}
				parts := []string{
					"fragment",
					"target_id=" + targetID,
					"target=" + fragment.targetSubelementName,
					fmt.Sprintf("bytes=%d", fragment.byteCount),
				}
				if fragment.rawHex != "" {
					parts = append(parts, "payload=0x"+hexPreview(fragment.rawHex, 32))
				}
				lines = append(lines, "  "+strings.Join(parts, " "))
			}
			if vendor := subelement.vendorSpecific; vendor != nil {
				parts := []string{"vendor", "oui=" + empty(vendor.oui, "<unknown>")}
				if vendor.vendorType != nil {
					parts = append(parts, fmt.Sprintf("type=%d", *vendor.vendorType))
				}
				parts = append(parts, fmt.Sprintf("payload_bytes=%d", vendor.payloadByteCount))
				if vendor.payloadHex != "" {
					parts = append(parts, "payload=0x"+hexPreview(vendor.payloadHex, 32))
				}
				lines = append(lines, "  "+strings.Join(parts, " "))
			}
			perSTA := subelement.perSTAProfile
			if perSTA == nil {
				continue
			}
			parts := []string{
				fmt.Sprintf("per_link link_id=%d", perSTA.linkID),
				fmt.Sprintf("control=0x%04x", perSTA.control),
				fmt.Sprintf("complete=%t", perSTA.completeProfile),
				"flags=" + joinedOrNone(perSTA.flags),
			}
			if perSTA.staInfoLength != nil {
				parts = append(parts, fmt.Sprintf("sta_info_len=%d", *perSTA.staInfoLength))
			}
			if perSTA.staMACAddress != "" {
				parts = append(parts, "sta_mac="+perSTA.staMACAddress)
			}
			if perSTA.beaconIntervalTU != nil {
				parts = append(parts, fmt.Sprintf("beacon_interval_tu=%d", *perSTA.beaconIntervalTU))
			}
			if perSTA.tsfOffsetHex != "" {
				parts = append(parts, "tsf_offset=0x"+perSTA.tsfOffsetHex)
			}
			if perSTA.dtimCount != nil || perSTA.dtimPeriod != nil {
				parts = append(parts, fmt.Sprintf("dtim=%s/%s", optionalInt(perSTA.dtimCount), optionalInt(perSTA.dtimPeriod)))
			}
			if perSTA.nstrLinkPairHex != "" {
				parts = append(parts, "nstr_link_pair=0x"+perSTA.nstrLinkPairHex)
			}
			if perSTA.bssParametersChangeCount != nil {
				parts = append(parts, fmt.Sprintf("bss_param_change_count=%d", *perSTA.bssParametersChangeCount))
			}
			parts = append(parts, fmt.Sprintf("profile_bytes=%d", perSTA.profileByteCount))
			if perSTA.truncated {
				parts = append(parts, "truncated=true")
			}
			lines = append(lines, "  "+strings.Join(parts, " "))
			for _, profile := range perSTA.profileElements {
				profileParts := []string{
					fmt.Sprintf("profile_ie link_id=%d", perSTA.linkID),
					fmt.Sprintf("id=%d", profile.id),
				}
				if profile.idExt != nil {
					profileParts = append(profileParts, fmt.Sprintf("ext=%d", *profile.idExt))
				}
				profileParts = append(profileParts,
					"name="+profile.name,
					fmt.Sprintf("len=%d", profile.declaredLength),
					fmt.Sprintf("actual=%d", profile.actualLength),
				)
				if profile.truncated {
					profileParts = append(profileParts, "truncated=true")
				}
				if profile.bodyHex != "" {
					profileParts = append(profileParts, "body=0x"+hexPreview(profile.bodyHex, 32))
				}
				lines = append(lines, "  "+strings.Join(profileParts, " "))
			}
			if security := wifiSecurityDetailsFromProfileElements(perSTA.profileElements); security != nil {
				lines = append(lines, fmt.Sprintf("  profile_security link_id=%d", perSTA.linkID))
				for _, line := range wifiSecuritySummaryLines(security) {
					lines = append(lines, "    "+line)
				}
			}
			if perSTA.profileElementsTruncated && len(perSTA.profileElements) == 0 && perSTA.profileHex != "" {
				lines = append(lines, fmt.Sprintf("  profile_unparsed link_id=%d bytes=%d body=0x%s", perSTA.linkID, perSTA.profileByteCount, hexPreview(perSTA.profileHex, 32)))
			}
		}
	}
	return lines
}

func parseEHTMultiLinkElement(bytes []byte, fallbackByteCount int) (ehtMultiLinkElement, bool) {
	if len(bytes) < 2 {
		return ehtMultiLinkElement{}, false
	}
	control := u16le(bytes, 0)
	typ := control & 0x7
	truncated := false
	offset := 2
	var commonInfo *ehtMultiLinkCommonInfo
	if offset < len(bytes) {
		commonLength := int(bytes[offset])
		commonStart := offset
		declaredCommonEnd := commonStart + commonLength
		commonEnd := min(declaredCommonEnd, len(bytes))
		if commonLength == 0 || declaredCommonEnd > len(bytes) {
			truncated = true
		}
		if typ == multiLinkTypeBasic {
			common, commonTruncated := parseBasicCommonInfo(bytes, commonStart, commonEnd, control, commonLength)
			commonInfo = &common
			truncated = truncated || commonTruncated
		} else {
			commonInfo = &ehtMultiLinkCommonInfo{length: commonLength}
		}
		offset = commonEnd
	}

	rawSubelements := []rawEHTMultiLinkSubelement{}
	for offset < len(bytes) {
		if offset+2 > len(bytes) {
			truncated = true
			break
		}
		id := int(bytes[offset])
		declaredLength := int(bytes[offset+1])
		dataStart := offset + 2
		dataEnd := min(dataStart+declaredLength, len(bytes))
		data := bytes[dataStart:dataEnd]
		subelementTruncated := len(data) != declaredLength
		truncated = truncated || subelementTruncated
		rawSubelements = append(rawSubelements, rawEHTMultiLinkSubelement{
			id:             id,
			declaredLength: declaredLength,
			data:           append([]byte(nil), data...),
			truncated:      subelementTruncated,
		})
		if subelementTruncated {
			break
		}
		offset = dataEnd
	}

	rawByteCount := fallbackByteCount
	if rawByteCount <= 0 {
		rawByteCount = len(bytes)
	}
	return ehtMultiLinkElement{
		control:      control,
		typ:          typ,
		typeName:     multiLinkTypeName(typ),
		presence:     basicMultiLinkPresence(control),
		commonInfo:   commonInfo,
		subelements:  parseEHTMultiLinkSubelements(rawSubelements),
		rawByteCount: rawByteCount,
		truncated:    truncated,
	}, true
}

type rawEHTMultiLinkSubelement struct {
	id             int
	declaredLength int
	data           []byte
	truncated      bool
}

func parseEHTMultiLinkSubelements(rawSubelements []rawEHTMultiLinkSubelement) []ehtMultiLinkSubelement {
	fragmentsByAnchor := map[int][][]byte{}
	fragmentTargets := map[int]*int{}
	var anchorIndex *int
	for index, raw := range rawSubelements {
		if isFragmentSubelement(raw.id) {
			fragmentTargets[index] = anchorIndex
			if anchorIndex != nil {
				fragmentsByAnchor[*anchorIndex] = append(fragmentsByAnchor[*anchorIndex], raw.data)
			}
			continue
		}
		value := index
		anchorIndex = &value
	}

	subelements := make([]ehtMultiLinkSubelement, 0, len(rawSubelements))
	for index, raw := range rawSubelements {
		fragments := fragmentsByAnchor[index]
		reassembled := append([]byte(nil), raw.data...)
		for _, fragment := range fragments {
			reassembled = append(reassembled, fragment...)
		}
		var reassembledLen *int
		if len(fragments) > 0 {
			value := len(reassembled)
			reassembledLen = &value
		}
		var perSTA *ehtPerSTAProfile
		if raw.id == perSTAProfileSubelementID {
			perSTA = parsePerSTAProfile(reassembled, raw.truncated)
		}
		var vendor *ehtVendorSpecificSubelement
		if isVendorSpecificSubelement(raw.id) {
			vendor = parseVendorSpecificSubelement(raw.data)
		}
		var fragment *ehtFragmentSubelement
		if isFragmentSubelement(raw.id) {
			var targetID *int
			targetName := "<none>"
			if targetIndex := fragmentTargets[index]; targetIndex != nil {
				id := rawSubelements[*targetIndex].id
				targetID = &id
				targetName = multiLinkSubelementName(id)
			}
			fragment = &ehtFragmentSubelement{
				targetSubelementID:   targetID,
				targetSubelementName: targetName,
				byteCount:            len(raw.data),
				rawHex:               hex.EncodeToString(raw.data),
			}
		}
		subelements = append(subelements, ehtMultiLinkSubelement{
			id:             raw.id,
			name:           multiLinkSubelementName(raw.id),
			declaredLength: raw.declaredLength,
			actualLength:   len(raw.data),
			perSTAProfile:  perSTA,
			vendorSpecific: vendor,
			fragment:       fragment,
			fragmentCount:  len(fragments),
			reassembledLen: reassembledLen,
			rawHex:         hex.EncodeToString(raw.data),
			truncated:      raw.truncated,
		})
	}
	return subelements
}

func parseBasicCommonInfo(bytes []byte, commonStart int, commonEnd int, control int, commonLength int) (ehtMultiLinkCommonInfo, bool) {
	offset := commonStart + 1
	truncated := false
	mldMAC := readMAC(bytes, offset, commonEnd)
	if mldMAC == "" {
		truncated = true
	}
	offset += 6
	var linkID *int
	if hasBit(control, 4) {
		linkID = readU8Ptr(bytes, offset, commonEnd, &truncated)
		if linkID != nil {
			*linkID &= 0x0f
		}
		offset++
	}
	var bssChangeCount *int
	if hasBit(control, 5) {
		bssChangeCount = readU8Ptr(bytes, offset, commonEnd, &truncated)
		offset++
	}
	mediumSync := ""
	if hasBit(control, 6) {
		mediumSync = readHex(bytes, offset, 2, commonEnd, &truncated)
		offset += 2
	}
	eml := ""
	if hasBit(control, 7) {
		eml = readHex(bytes, offset, 2, commonEnd, &truncated)
		offset += 2
	}
	mldCaps := ""
	if hasBit(control, 8) {
		mldCaps = readHex(bytes, offset, 2, commonEnd, &truncated)
		offset += 2
	}
	var apMLDID *int
	if hasBit(control, 9) {
		apMLDID = readU8Ptr(bytes, offset, commonEnd, &truncated)
	}
	return ehtMultiLinkCommonInfo{
		length:                            commonLength,
		mldMACAddress:                     mldMAC,
		linkID:                            linkID,
		bssParametersChangeCount:          bssChangeCount,
		mediumSynchronizationDelayInfoHex: mediumSync,
		emlCapabilitiesHex:                eml,
		mldCapabilitiesAndOperationsHex:   mldCaps,
		apMLDID:                           apMLDID,
	}, truncated
}

func parsePerSTAProfile(data []byte, alreadyTruncated bool) *ehtPerSTAProfile {
	if len(data) < 3 {
		return nil
	}
	control := u16le(data, 0)
	staInfoLengthValue := int(data[2])
	staInfoLength := staInfoLengthValue
	staInfoLengthPtr := &staInfoLength
	staInfoStart := 3
	staInfoEnd := min(2+staInfoLengthValue, len(data))
	offset := staInfoStart
	truncated := alreadyTruncated || staInfoLengthValue == 0 || 2+staInfoLengthValue > len(data)
	staMAC := ""
	if hasBit(control, 5) {
		staMAC = readMAC(data, offset, staInfoEnd)
		if staMAC == "" {
			truncated = true
		}
		offset += 6
	}
	var beaconInterval *int
	if hasBit(control, 6) {
		beaconInterval = readU16Ptr(data, offset, staInfoEnd, &truncated)
		offset += 2
	}
	tsfOffset := ""
	if hasBit(control, 7) {
		tsfOffset = readHex(data, offset, 8, staInfoEnd, &truncated)
		offset += 8
	}
	var dtimCount *int
	var dtimPeriod *int
	if hasBit(control, 8) {
		dtimCount = readU8Ptr(data, offset, staInfoEnd, &truncated)
		dtimPeriod = readU8Ptr(data, offset+1, staInfoEnd, &truncated)
		offset += 2
	}
	nstrLinkPair := ""
	if hasBit(control, 9) {
		length := 1
		if hasBit(control, 10) {
			length = 2
		}
		nstrLinkPair = readHex(data, offset, length, staInfoEnd, &truncated)
		offset += length
	}
	var bssChangeCount *int
	if hasBit(control, 11) {
		bssChangeCount = readU8Ptr(data, offset, staInfoEnd, &truncated)
	}
	profileStart := min(staInfoEnd, len(data))
	profile := data[profileStart:]
	profileElements, profileTruncated := parseProfileInformationElements(profile)
	return &ehtPerSTAProfile{
		control:                  control,
		linkID:                   control & 0x0f,
		completeProfile:          hasBit(control, 4),
		flags:                    perSTAProfileFlags(control),
		staInfoLength:            staInfoLengthPtr,
		staMACAddress:            staMAC,
		beaconIntervalTU:         beaconInterval,
		tsfOffsetHex:             tsfOffset,
		dtimCount:                dtimCount,
		dtimPeriod:               dtimPeriod,
		nstrLinkPairHex:          nstrLinkPair,
		bssParametersChangeCount: bssChangeCount,
		profileByteCount:         len(profile),
		profileHex:               hex.EncodeToString(profile),
		profileElements:          profileElements,
		profileElementsTruncated: profileTruncated,
		truncated:                truncated,
	}
}

func parseVendorSpecificSubelement(data []byte) *ehtVendorSpecificSubelement {
	oui := ""
	if len(data) >= 3 {
		oui = fmt.Sprintf("%02x:%02x:%02x", data[0], data[1], data[2])
	}
	var vendorType *int
	if len(data) >= 4 {
		value := int(data[3])
		vendorType = &value
	}
	payloadStart := min(len(data), 4)
	payload := data[payloadStart:]
	return &ehtVendorSpecificSubelement{
		oui:              oui,
		vendorType:       vendorType,
		payloadByteCount: len(payload),
		payloadHex:       hex.EncodeToString(payload),
	}
}

func parseProfileInformationElements(data []byte) ([]ehtProfileInformationElement, bool) {
	if len(data) == 0 {
		return nil, false
	}
	elements := []ehtProfileInformationElement{}
	truncated := false
	offset := 0
	for offset < len(data) {
		if offset+2 > len(data) {
			truncated = true
			break
		}
		id := int(data[offset])
		declaredLength := int(data[offset+1])
		bodyStart := offset + 2
		bodyEnd := min(bodyStart+declaredLength, len(data))
		body := data[bodyStart:bodyEnd]
		elementTruncated := len(body) != declaredLength
		truncated = truncated || elementTruncated
		var idExt *int
		if id == ehtMultiLinkElementID && len(body) > 0 {
			value := int(body[0])
			idExt = &value
		}
		elements = append(elements, ehtProfileInformationElement{
			id:             id,
			idExt:          idExt,
			name:           profileInformationElementName(id, idExt),
			declaredLength: declaredLength,
			actualLength:   len(body),
			bodyHex:        hex.EncodeToString(body),
			truncated:      elementTruncated,
		})
		if elementTruncated {
			break
		}
		offset = bodyEnd
	}
	return elements, truncated
}

func basicMultiLinkPresence(control int) []string {
	presence := []string{}
	if hasBit(control, 4) {
		presence = append(presence, "link_id_info")
	}
	if hasBit(control, 5) {
		presence = append(presence, "bss_parameters_change_count")
	}
	if hasBit(control, 6) {
		presence = append(presence, "medium_synchronization_delay")
	}
	if hasBit(control, 7) {
		presence = append(presence, "eml_capabilities")
	}
	if hasBit(control, 8) {
		presence = append(presence, "mld_capabilities_and_operations")
	}
	if hasBit(control, 9) {
		presence = append(presence, "ap_mld_id")
	}
	return presence
}

func perSTAProfileFlags(control int) []string {
	flags := []string{}
	if hasBit(control, 4) {
		flags = append(flags, "complete_profile")
	}
	if hasBit(control, 5) {
		flags = append(flags, "mac_address")
	}
	if hasBit(control, 6) {
		flags = append(flags, "beacon_interval")
	}
	if hasBit(control, 7) {
		flags = append(flags, "tsf_offset")
	}
	if hasBit(control, 8) {
		flags = append(flags, "dtim_info")
	}
	if hasBit(control, 9) {
		flags = append(flags, "nstr_link_pair")
	}
	if hasBit(control, 10) {
		flags = append(flags, "nstr_bitmap_size")
	}
	if hasBit(control, 11) {
		flags = append(flags, "bss_parameters_change_count")
	}
	return flags
}

func multiLinkTypeName(typ int) string {
	switch typ {
	case 0:
		return "basic"
	case 1:
		return "probe_request"
	case 2:
		return "reconfiguration"
	case 3:
		return "tdls"
	case 4:
		return "priority_access"
	default:
		return "reserved"
	}
}

func multiLinkSubelementName(id int) string {
	switch id {
	case 0:
		return "per_sta_profile"
	case legacyFragmentSubelementID:
		return "fragment_legacy"
	case legacyVendorSpecificSubelementID:
		return "vendor_specific_legacy"
	case vendorSpecificSubelementID:
		return "vendor_specific"
	case fragmentSubelementID:
		return "fragment"
	default:
		return fmt.Sprintf("subelement_%d", id)
	}
}

func isFragmentSubelement(id int) bool {
	return id == fragmentSubelementID || id == legacyFragmentSubelementID
}

func isVendorSpecificSubelement(id int) bool {
	return id == vendorSpecificSubelementID || id == legacyVendorSpecificSubelementID
}

func profileInformationElementName(id int, idExt *int) string {
	if id == ehtMultiLinkElementID {
		if idExt == nil {
			return "extension"
		}
		switch *idExt {
		case 106:
			return "eht_operation"
		case 107:
			return "eht_multi_link"
		case 108:
			return "eht_capabilities"
		case 56:
			return "non_inheritance"
		default:
			return fmt.Sprintf("extension_%d", *idExt)
		}
	}
	switch id {
	case 0:
		return "ssid"
	case 1:
		return "supported_rates"
	case 3:
		return "dsss_parameter_set"
	case 5:
		return "tim"
	case 7:
		return "country"
	case 11:
		return "bss_load"
	case 32:
		return "power_constraint"
	case 35:
		return "tpc_report"
	case 45:
		return "ht_capabilities"
	case 48:
		return "rsn"
	case 50:
		return "extended_supported_rates"
	case 61:
		return "ht_operation"
	case 70:
		return "radio_measurement_11k"
	case 74:
		return "overlapping_bss_scan_parameters"
	case 127:
		return "extended_capabilities"
	case 244:
		return "rsnxe"
	case 191:
		return "vht_capabilities"
	case 192:
		return "vht_operation"
	case 201:
		return "reduced_neighbor_report"
	case 221:
		return "vendor_specific"
	default:
		return fmt.Sprintf("element_%d", id)
	}
}

func readMAC(bytes []byte, offset int, limit int) string {
	if offset+6 > limit || offset+6 > len(bytes) {
		return ""
	}
	parts := make([]string, 6)
	for i := range 6 {
		parts[i] = fmt.Sprintf("%02x", bytes[offset+i])
	}
	return strings.Join(parts, ":")
}

func readHex(bytes []byte, offset int, length int, limit int, truncated *bool) string {
	if length <= 0 || offset+length > limit || offset+length > len(bytes) {
		*truncated = true
		return ""
	}
	return hex.EncodeToString(bytes[offset : offset+length])
}

func readU8Ptr(bytes []byte, offset int, limit int, truncated *bool) *int {
	if offset >= limit || offset >= len(bytes) {
		*truncated = true
		return nil
	}
	value := int(bytes[offset])
	return &value
}

func readU16Ptr(bytes []byte, offset int, limit int, truncated *bool) *int {
	if offset+2 > limit || offset+2 > len(bytes) {
		*truncated = true
		return nil
	}
	value := u16le(bytes, offset)
	return &value
}

func u16le(bytes []byte, offset int) int {
	return int(bytes[offset]) | int(bytes[offset+1])<<8
}

func hasBit(value int, bit int) bool {
	return value&(1<<bit) != 0
}

func joinedOrNone(values []string) string {
	if len(values) == 0 {
		return "<none>"
	}
	return strings.Join(values, ",")
}

func optionalInt(value *int) string {
	if value == nil {
		return "?"
	}
	return fmt.Sprint(*value)
}

func hexPreview(value string, maxBytes int) string {
	maxChars := maxBytes * 2
	if len(value) <= maxChars {
		return value
	}
	remainingBytes := (len(value) - maxChars) / 2
	return fmt.Sprintf("%s...(+%dB)", value[:maxChars], remainingBytes)
}
