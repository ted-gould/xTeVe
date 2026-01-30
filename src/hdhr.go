package src

import (
	"bytes"
	"cmp"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"maps"
	"slices"
	"strconv"
)

func makeInteraceFromHDHR(content []byte, playlistName, id string) (channels []any, err error) {
	var hdhrData []any

	err = json.Unmarshal(content, &hdhrData)
	if err == nil {
		for _, d := range hdhrData {
			var channel = make(map[string]string)
			var data, ok = d.(map[string]any)
			if !ok {
				continue
			}

			channel["group-title"] = playlistName
			if guideName, ok := data["GuideName"].(string); ok {
				channel["name"] = guideName
				channel["tvg-id"] = guideName
			}
			if guideURL, ok := data["URL"].(string); ok {
				channel["url"] = guideURL
			}
			if guideNumber, ok := data["GuideNumber"].(string); ok {
				channel["ID-"+id] = guideNumber
			}
			channel["_uuid.key"] = "ID-" + id
			channel["_values"] = playlistName + " " + channel["name"]

			channels = append(channels, channel)
		}
	}
	return
}

func getCapability(domain string) (xmlContent []byte, err error) {
	var capability Capability
	var buffer bytes.Buffer
	var targetDomain = System.Domain

	if len(domain) > 0 {
		targetDomain = domain
	}

	capability.Xmlns = "urn:schemas-upnp-org:device-1-0"
	capability.URLBase = System.ServerProtocol.WEB + "://" + targetDomain

	capability.SpecVersion.Major = 1
	capability.SpecVersion.Minor = 0

	capability.Device.DeviceType = "urn:schemas-upnp-org:device:MediaServer:1"
	capability.Device.FriendlyName = System.Name
	capability.Device.Manufacturer = "Silicondust"
	capability.Device.ModelName = "HDTC-2US"
	capability.Device.ModelNumber = "HDTC-2US"
	capability.Device.SerialNumber = ""
	capability.Device.UDN = "uuid:" + System.DeviceID

	output, err := xml.MarshalIndent(capability, " ", "  ")
	if err != nil {
		ShowError(err, 1003)
	}

	buffer.Write([]byte(xml.Header))
	buffer.Write([]byte(output))
	xmlContent = buffer.Bytes()
	return
}

func getDiscover(domain string) (jsonContent []byte, err error) {
	var discover Discover
	var targetDomain = System.Domain

	if len(domain) > 0 {
		targetDomain = domain
	}

	discover.BaseURL = System.ServerProtocol.WEB + "://" + targetDomain
	discover.DeviceAuth = System.AppName
	discover.DeviceID = System.DeviceID
	discover.FirmwareName = "bin_" + System.Version
	discover.FirmwareVersion = System.Version
	discover.FriendlyName = System.Name

	discover.LineupURL = fmt.Sprintf("%s://%s/lineup.json", System.ServerProtocol.DVR, targetDomain)
	discover.Manufacturer = "Golang"
	discover.ModelNumber = System.Version
	discover.TunerCount = Settings.Tuner

	jsonContent, err = json.MarshalIndent(discover, "", "  ")
	return
}

func getLineupStatus() (jsonContent []byte, err error) {
	var lineupStatus LineupStatus

	lineupStatus.ScanInProgress = System.ScanInProgress
	lineupStatus.ScanPossible = 0
	lineupStatus.Source = "Cable"
	lineupStatus.SourceList = []string{"Cable"}

	jsonContent, err = json.MarshalIndent(lineupStatus, "", "  ")
	return
}

func getLineup(domain string) (jsonContent []byte, err error) {
	var lineup Lineup
	var targetDomain = System.Domain

	if len(domain) > 0 {
		targetDomain = domain
	}

	switch Settings.EpgSource {
	case "PMS":
		for i, dsa := range Data.Streams.Active {
			var m3uChannel M3UChannelStructXEPG

			// Optimization: Manual assignment for map[string]string avoids JSON overhead
			// bindMapToM3UChannelStruct is ~57x faster than bindToStruct (249ns vs 14384ns)
			// and does not return an error as it performs direct field assignment.
			if streamMap, ok := dsa.(map[string]string); ok {
				bindMapToM3UChannelStruct(streamMap, &m3uChannel)
			} else {
				err = bindToStruct(dsa, &m3uChannel)
				if err != nil {
					return
				}
			}

			var stream LineupStream
			stream.GuideName = m3uChannel.Name
			switch len(m3uChannel.UUIDValue) {
			case 0:
				stream.GuideNumber = fmt.Sprintf("%d", i+1000)
				guideNumber, err := getGuideNumberPMS(stream.GuideName)
				if err != nil {
					ShowError(err, 0)
				}
				stream.GuideNumber = guideNumber
			default:
				stream.GuideNumber = m3uChannel.UUIDValue
			}

			stream.URL, err = createStreamingURL(targetDomain, "DVR", m3uChannel.FileM3UID, stream.GuideNumber, m3uChannel.Name, m3uChannel.URL)
			if err == nil {
				lineup = append(lineup, stream)
			} else {
				ShowError(err, 1202)
			}
		}
	case "XEPG":
		for _, xepgChannel := range Data.XEPG.Channels {

			if xepgChannel.XActive {
				var stream LineupStream
				stream.GuideName = xepgChannel.XName
				stream.GuideNumber = xepgChannel.XChannelID
				//stream.URL = fmt.Sprintf("%s://%s/stream/%s-%s", System.ServerProtocol.DVR, System.Domain, xepgChannel.FileM3UID, base64.StdEncoding.EncodeToString([]byte(xepgChannel.URL)))
				stream.URL, err = createStreamingURL(targetDomain, "DVR", xepgChannel.FileM3UID, xepgChannel.XChannelID, xepgChannel.XName, xepgChannel.URL)
				if err == nil {
					lineup = append(lineup, stream)
				} else {
					ShowError(err, 1202)
				}
			}
		}
	}

	// Sort the lineup
	slices.SortFunc(lineup, func(a, b LineupStream) int {
		chanA, _ := strconv.ParseFloat(a.GuideNumber, 64)
		chanB, _ := strconv.ParseFloat(b.GuideNumber, 64)
		return cmp.Compare(chanA, chanB)
	})

	jsonContent, err = json.MarshalIndent(lineup, "", "  ")
	if err != nil {
		return
	}

	Data.Cache.PMS = nil

	err = saveMapToJSONFile(System.File.URLS, Data.Cache.StreamingURLS)
	return
}

func getGuideNumberPMS(channelName string) (pmsID string, err error) {
	if len(Data.Cache.PMS) == 0 {
		Data.Cache.PMS = make(map[string]string)

		pms, err := loadJSONFileToMap(System.File.PMS)

		if err != nil {
			return "", err
		}

		for key, value := range pms {
			if v, ok := value.(string); ok {
				Data.Cache.PMS[key] = v
			}
		}
	}

	var getNewID = func(channelName string) (id string) {
		var i int
		ids := slices.Collect(maps.Values(Data.Cache.PMS))

		for {
			id = fmt.Sprintf("id-%d", i)
			if !slices.Contains(ids, id) {
				return
			}
			i++
		}
	}

	if value, ok := Data.Cache.PMS[channelName]; ok {
		pmsID = value
	} else {
		pmsID = getNewID(channelName)
		Data.Cache.PMS[channelName] = pmsID
		err = saveMapToJSONFile(System.File.PMS, Data.Cache.PMS)
	}
	return
}
