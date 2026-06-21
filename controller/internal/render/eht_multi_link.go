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
	extendedMLDCapabilitiesHex        string
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
			if common.extendedMLDCapabilitiesHex != "" {
				parts = append(parts, "ext_mld_capabilities=0x"+common.extendedMLDCapabilitiesHex)
			}
			lines = append(lines, "  "+strings.Join(parts, " "))
			lines = append(lines, multiLinkCommonCapabilityLines(common)...)
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
				parts = append(parts, "nstr_bitmap_bits="+bitmapBitsFromHex(perSTA.nstrLinkPairHex))
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
			for _, line := range wifiMLOProfileDecodeLines(fmt.Sprintf("profile_decode link_id=%d", perSTA.linkID), perSTA.profileElements) {
				lines = append(lines, "  "+line)
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
		offset++
	}
	extendedMLDCaps := ""
	if hasBit(control, 10) {
		extendedMLDCaps = readHex(bytes, offset, 2, commonEnd, &truncated)
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
		extendedMLDCapabilitiesHex:        extendedMLDCaps,
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
	if hasBit(control, 10) {
		presence = append(presence, "extended_mld_capabilities_and_operations")
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

func multiLinkCommonCapabilityLines(common *ehtMultiLinkCommonInfo) []string {
	lines := []string{}
	if value, ok := u16HexValue(common.mediumSynchronizationDelayInfoHex); ok {
		lines = append(lines, fmt.Sprintf("  medium_sync raw=0x%s duration=%d ofdm_ed_threshold=%d max_txop=%d",
			common.mediumSynchronizationDelayInfoHex,
			value&0x00ff,
			(value>>8)&0x0f,
			(value>>12)&0x0f,
		))
	}
	if value, ok := u16HexValue(common.emlCapabilitiesHex); ok {
		flags := []string{}
		if value&0x0001 != 0 {
			flags = append(flags, "emlsr")
		}
		if value&0x0080 != 0 {
			flags = append(flags, "emlmr")
		}
		reserved := value & 0x8700
		suffix := ""
		if reserved != 0 {
			suffix = fmt.Sprintf(" reserved=0x%04x", reserved)
		}
		lines = append(lines, fmt.Sprintf("  eml raw=0x%s flags=%s emlsr_padding_delay=%d emlsr_transition_delay=%d transition_timeout=%d%s",
			common.emlCapabilitiesHex,
			joinedOrNone(flags),
			(value&0x000e)>>1,
			(value&0x0070)>>4,
			(value&0x7800)>>11,
			suffix,
		))
	}
	if value, ok := u16HexValue(common.mldCapabilitiesAndOperationsHex); ok {
		flags := []string{}
		if value&0x0010 != 0 {
			flags = append(flags, "srs")
		}
		if value&0x1000 != 0 {
			flags = append(flags, "aar")
		}
		if value&0x2000 != 0 {
			flags = append(flags, "link_reconfig")
		}
		if value&0x4000 != 0 {
			flags = append(flags, "aligned_twt")
		}
		tidToLink := []string{}
		if value&0x0020 != 0 {
			tidToLink = append(tidToLink, "all_to_all")
		}
		if value&0x0040 != 0 {
			tidToLink = append(tidToLink, "all_to_one")
		}
		suffix := ""
		if value&0x8000 != 0 {
			suffix = " reserved=0x8000"
		}
		lines = append(lines, fmt.Sprintf("  mld raw=0x%s flags=%s max_sim_links_code=%d tid_to_link=%s ap_mld_type=%d freq_sep_for_str_code=%d%s",
			common.mldCapabilitiesAndOperationsHex,
			joinedOrNone(flags),
			value&0x000f,
			joinedOrNone(tidToLink),
			(value&0x0080)>>7,
			(value&0x0f80)>>7,
			suffix,
		))
	}
	if value, ok := u16HexValue(common.extendedMLDCapabilitiesHex); ok {
		flags := []string{}
		if value&0x0001 != 0 {
			flags = append(flags, "op_param_update")
		}
		if value&0x0020 != 0 {
			flags = append(flags, "nstr_update")
		}
		if value&0x0040 != 0 {
			flags = append(flags, "emlsr_enabled_one_link")
		}
		if value&0x0080 != 0 {
			flags = append(flags, "btm_mld_recommendation_multi_ap")
		}
		suffix := ""
		if value&0xff00 != 0 {
			suffix = fmt.Sprintf(" reserved=0x%04x", value&0xff00)
		}
		lines = append(lines, fmt.Sprintf("  ext_mld raw=0x%s flags=%s recommended_max_links_code=%d%s",
			common.extendedMLDCapabilitiesHex,
			joinedOrNone(flags),
			(value&0x001e)>>1,
			suffix,
		))
	}
	return lines
}

func profileElementsAsWifiInformationElements(elements []ehtProfileInformationElement) []*controlpb.WifiInformationElement {
	converted := make([]*controlpb.WifiInformationElement, 0, len(elements))
	for _, element := range elements {
		body, err := hex.DecodeString(element.bodyHex)
		if err != nil {
			continue
		}
		item := &controlpb.WifiInformationElement{Id: int32(element.id)}
		if element.id == ehtMultiLinkElementID && element.idExt != nil {
			if len(body) == 0 {
				continue
			}
			item.IdExt = int32(*element.idExt)
			item.ByteCount = uint32(max(0, element.actualLength-1))
			item.BytesHex = hex.EncodeToString(body[1:])
		} else {
			item.ByteCount = uint32(element.actualLength)
			item.BytesHex = element.bodyHex
		}
		converted = append(converted, item)
	}
	return converted
}

func wifiMLOProfileDecodeLines(label string, elements []ehtProfileInformationElement) []string {
	converted := profileElementsAsWifiInformationElements(elements)
	mles := parseEHTMultiLinkElements(converted)
	lines := []string{}
	for _, element := range elements {
		switch {
		case element.id == ehtMultiLinkElementID && element.idExt != nil && *element.idExt == 36:
			lines = append(lines, profileHEOperationLines(element.bodyHex)...)
		case element.id == ehtMultiLinkElementID && element.idExt != nil && *element.idExt == 39:
			lines = append(lines, profileHESpatialReuseLines(element.bodyHex)...)
		case element.id == ehtMultiLinkElementID && element.idExt != nil && *element.idExt == 59:
			lines = append(lines, profileHE6GHzLines(element.bodyHex)...)
		case element.id == ehtMultiLinkElementID && element.idExt != nil && *element.idExt == 106:
			lines = append(lines, profileEHTOperationLines(element.bodyHex)...)
		case element.id == ehtMultiLinkElementID && element.idExt != nil && *element.idExt == 107:
			// The parsed summary below covers Multi-Link elements.
		case element.id == ehtMultiLinkElementID && element.idExt != nil && *element.idExt == 108:
			lines = append(lines, profileEHTCapabilitiesLines(element.bodyHex)...)
		}
	}
	if len(mles) > 0 {
		mld := ""
		linkIDs := []string{}
		for _, mle := range mles {
			if mld == "" && mle.commonInfo != nil {
				mld = mle.commonInfo.mldMACAddress
			}
			if mle.commonInfo != nil && mle.commonInfo.linkID != nil {
				linkIDs = append(linkIDs, fmt.Sprint(*mle.commonInfo.linkID))
			}
			for _, subelement := range mle.subelements {
				if subelement.perSTAProfile != nil {
					linkIDs = append(linkIDs, fmt.Sprint(subelement.perSTAProfile.linkID))
				}
			}
		}
		lines = append(lines, fmt.Sprintf("eht_mle count=%d ap_mld=%s link_ids=%s", len(mles), empty(mld, "<unknown>"), wifiMLOJoinStrings(linkIDs, "<none>")))
	}
	if len(lines) == 0 {
		return nil
	}
	out := []string{label}
	for _, line := range lines {
		out = append(out, "  "+line)
	}
	return out
}

func profileHEOperationLines(bodyHex string) []string {
	body, ok := extensionBodyPayload(bodyHex)
	if !ok || len(body) < 6 {
		return []string{fmt.Sprintf("he_operation_warnings he_operation_too_short bytes=%d required=6", len(body))}
	}
	parameters := int(u32le(body, 0))
	lines := []string{
		fmt.Sprintf("he_operation bss_color=%d disabled=%t flags=%s",
			(parameters>>24)&0x3f,
			parameters&0x80000000 != 0,
			wifiMLOJoinStrings(heOperationFlagNames(parameters), "<none>"),
		),
	}
	offset := 6
	if parameters&0x00004000 != 0 {
		offset += 3
	}
	if parameters&0x00008000 != 0 {
		offset++
	}
	if parameters&0x00020000 != 0 {
		if len(body) < offset+5 {
			lines = append(lines, fmt.Sprintf("he_operation_warnings he_6ghz_operation_too_short bytes=%d required=5", max(0, len(body)-offset)))
		} else {
			control := int(body[offset+1])
			lines = append(lines, fmt.Sprintf("he_operation_6ghz width=%s primary=%d ccfs0=%d ccfs1=%d",
				he6GHzChannelWidthName(control&0x03),
				body[offset],
				body[offset+2],
				body[offset+3],
			))
		}
	}
	return lines
}

func profileHESpatialReuseLines(bodyHex string) []string {
	body, ok := extensionBodyPayload(bodyHex)
	if !ok || len(body) == 0 {
		return []string{fmt.Sprintf("spatial_reuse_warnings he_spatial_reuse_too_short bytes=%d required=1", len(body))}
	}
	return []string{fmt.Sprintf("spatial_reuse control=0x%x flags=%s", body[0], wifiMLOJoinStrings(heSpatialReuseFlagNames(int(body[0])), "<none>"))}
}

func profileHE6GHzLines(bodyHex string) []string {
	body, ok := extensionBodyPayload(bodyHex)
	if !ok || len(body) < 2 {
		return []string{fmt.Sprintf("he_6ghz_warnings he_6ghz_capabilities_too_short bytes=%d required=2", len(body))}
	}
	capabilities := u16le(body, 0)
	return []string{fmt.Sprintf("he_6ghz max_mpdu=%d max_ampdu=%d smps=%s",
		maxMPDULengthBytes((capabilities>>6)&0x03),
		maxAMPDULengthBytes((capabilities>>3)&0x07),
		heSMPowerSaveName((capabilities>>9)&0x03),
	)}
}

func profileEHTOperationLines(bodyHex string) []string {
	body, ok := extensionBodyPayload(bodyHex)
	if !ok || len(body) < 5 {
		return []string{fmt.Sprintf("eht_operation_warnings eht_operation_too_short bytes=%d required=5", len(body))}
	}
	parameters := int(body[0])
	line := fmt.Sprintf("eht_operation op_info=%t width=%s flags=%s",
		parameters&0x01 != 0,
		"<unknown>",
		wifiMLOJoinStrings(ehtOperationFlagNames(parameters), "<none>"),
	)
	lines := []string{line}
	offset := 5
	if parameters&0x01 != 0 && len(body) >= offset+3 {
		control := int(body[offset])
		lines[0] = fmt.Sprintf("eht_operation op_info=true width=%s flags=%s",
			ehtChannelWidthName(control&0x07),
			wifiMLOJoinStrings(ehtOperationFlagNames(parameters), "<none>"),
		)
		offset += 3
		if parameters&0x02 != 0 && len(body) >= offset+2 {
			bitmap := u16le(body, offset)
			lines = append(lines, fmt.Sprintf("eht_operation disabled=0x%04x punctured=%s", bitmap, disabledSubchannelIndicesSummary(bitmap, control&0x07)))
		}
	}
	return lines
}

func profileEHTCapabilitiesLines(bodyHex string) []string {
	body, ok := extensionBodyPayload(bodyHex)
	if !ok || len(body) < 11 {
		return []string{fmt.Sprintf("eht_capabilities_warnings eht_capabilities_too_short bytes=%d required=11", len(body))}
	}
	mac := body[:2]
	phy := body[2:11]
	features := []string{}
	if mac[0]&0x02 != 0 {
		features = append(features, "om_control")
	}
	if mac[0]&0x10 != 0 {
		features = append(features, "restricted_twt")
	}
	if phy[0]&0x02 != 0 {
		features = append(features, "320mhz_in_6ghz")
	}
	if phy[4]&0x02 != 0 {
		features = append(features, "psr_spatial_reuse")
	}
	if phy[6]&0x80 != 0 {
		features = append(features, "eht_duplicate_6ghz")
	}
	if phy[8]&0x02 != 0 {
		features = append(features, "rx_4096qam_wider_bw_dl_ofdma")
	}
	return []string{"eht_capabilities features=" + wifiMLOJoinStrings(features, "<none>")}
}

func extensionBodyPayload(bodyHex string) ([]byte, bool) {
	body, err := hex.DecodeString(bodyHex)
	if err != nil || len(body) == 0 {
		return nil, false
	}
	return body[1:], true
}

func heOperationFlagNames(parameters int) []string {
	flags := []string{fmt.Sprintf("default_pe_duration=%d", parameters&0x07), fmt.Sprintf("rts_threshold=%d", (parameters>>4)&0x03ff)}
	if parameters&0x00000008 != 0 {
		flags = append(flags, "twt_required")
	}
	if parameters&0x00004000 != 0 {
		flags = append(flags, "vht_operation_info_present")
	}
	if parameters&0x00008000 != 0 {
		flags = append(flags, "co_hosted_bss")
	}
	if parameters&0x00010000 != 0 {
		flags = append(flags, "er_su_disable")
	}
	if parameters&0x00020000 != 0 {
		flags = append(flags, "6ghz_operation_info_present")
	}
	if parameters&0x40000000 != 0 {
		flags = append(flags, "partial_bss_color")
	}
	if parameters&0x80000000 != 0 {
		flags = append(flags, "bss_color_disabled")
	}
	return flags
}

func ehtOperationFlagNames(parameters int) []string {
	flags := []string{}
	if parameters&0x01 != 0 {
		flags = append(flags, "operation_information_present")
	}
	if parameters&0x02 != 0 {
		flags = append(flags, "disabled_subchannel_bitmap_present")
	}
	if parameters&0x04 != 0 {
		flags = append(flags, "eht_default_pe_duration")
	}
	if parameters&0x08 != 0 {
		flags = append(flags, "group_addressed_bu_indication_limit")
	}
	flags = append(flags, fmt.Sprintf("group_addressed_bu_indication_exponent=%d", (parameters>>4)&0x03))
	if parameters&0x40 != 0 {
		flags = append(flags, "mcs15_disabled")
	}
	return flags
}

func heSpatialReuseFlagNames(control int) []string {
	flags := []string{}
	if control&0x01 != 0 {
		flags = append(flags, "psr_disallowed")
	}
	if control&0x02 != 0 {
		flags = append(flags, "non_srg_obss_pd_sr_disallowed")
	}
	if control&0x04 != 0 {
		flags = append(flags, "non_srg_obss_pd_max_offset_present")
	}
	if control&0x08 != 0 {
		flags = append(flags, "srg_information_present")
	}
	if control&0x10 != 0 {
		flags = append(flags, "hesiga_spatial_reuse_value15_allowed")
	}
	return flags
}

func he6GHzChannelWidthName(code int) string {
	switch code {
	case 0:
		return "20MHz"
	case 1:
		return "40MHz"
	case 2:
		return "80MHz"
	case 3:
		return "160MHz/80+80MHz"
	default:
		return "<unknown>"
	}
}

func ehtChannelWidthName(code int) string {
	switch code {
	case 0:
		return "20MHz"
	case 1:
		return "40MHz"
	case 2:
		return "80MHz"
	case 3:
		return "160MHz"
	case 4:
		return "320MHz"
	default:
		return fmt.Sprintf("reserved_%d", code)
	}
}

func disabledSubchannelIndicesSummary(bitmap int, _ int) string {
	indices := []string{}
	for i := range 16 {
		if bitmap&(1<<i) != 0 {
			indices = append(indices, fmt.Sprint(i))
		}
	}
	return wifiMLOJoinStrings(indices, "<none>")
}

func maxAMPDULengthBytes(exponent int) int {
	return (1 << (13 + exponent)) - 1
}

func maxMPDULengthBytes(code int) int {
	switch code {
	case 0:
		return 3895
	case 1:
		return 7991
	case 2:
		return 11454
	default:
		return 0
	}
}

func heSMPowerSaveName(code int) string {
	switch code {
	case 0:
		return "static"
	case 1:
		return "dynamic"
	case 3:
		return "disabled"
	default:
		return "reserved"
	}
}

func u16HexValue(value string) (int, bool) {
	bytes, err := hex.DecodeString(value)
	if err != nil || len(bytes) < 2 {
		return 0, false
	}
	return u16le(bytes, 0), true
}

func bitmapBitsFromHex(value string) string {
	bytes, err := hex.DecodeString(value)
	if err != nil {
		return "<none>"
	}
	bits := []string{}
	for byteIndex, b := range bytes {
		for bit := range 8 {
			if int(b)&(1<<bit) != 0 {
				bits = append(bits, fmt.Sprint(byteIndex*8+bit))
			}
		}
	}
	return wifiMLOJoinStrings(bits, "<none>")
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

func u32le(bytes []byte, offset int) uint32 {
	return uint32(bytes[offset]) |
		uint32(bytes[offset+1])<<8 |
		uint32(bytes[offset+2])<<16 |
		uint32(bytes[offset+3])<<24
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
