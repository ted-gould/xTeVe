package src

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
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

func getCapability() (xmlContent []byte, err error) {
	var capability Capability
	var buffer bytes.Buffer

	capability.Xmlns = "urn:schemas-upnp-org:device-1-0"
	capability.URLBase = System.ServerProtocol.WEB + "://" + System.Domain

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

func getDiscover() (jsonContent []byte, err error) {
	var discover Discover

	discover.BaseURL = System.ServerProtocol.WEB + "://" + System.Domain
	discover.DeviceAuth = System.AppName
	discover.DeviceID = System.DeviceID
	discover.FirmwareName = "bin_" + System.Version
	discover.FirmwareVersion = System.Version
	discover.FriendlyName = System.Name

	discover.LineupURL = fmt.Sprintf("%s://%s/lineup.json", System.ServerProtocol.DVR, System.Domain)
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

func getLineup() (jsonContent []byte, err error) {
	var lineup Lineup

	switch Settings.EpgSource {
	case "PMS":
		for i, dsa := range Data.Streams.Active {
			var m3uChannel M3UChannelStructXEPG

			err = bindToStruct(dsa, &m3uChannel)
			if err != nil {
				return
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

			stream.URL, err = createStreamingURL("DVR", m3uChannel.FileM3UID, stream.GuideNumber, m3uChannel.Name, m3uChannel.URL)
			if err == nil {
				lineup = append(lineup, stream)
			} else {
				ShowError(err, 1202)
			}
		}
	case "XEPG":
		for _, dxc := range Data.XEPG.Channels {
			var xepgChannel XEPGChannelStruct
			err = bindToStruct(dxc, &xepgChannel)
			if err != nil {
				return
			}

			if xepgChannel.XActive {
				var stream LineupStream
				stream.GuideName = xepgChannel.XName
				stream.GuideNumber = xepgChannel.XChannelID
				//stream.URL = fmt.Sprintf("%s://%s/stream/%s-%s", System.ServerProtocol.DVR, System.Domain, xepgChannel.FileM3UID, base64.StdEncoding.EncodeToString([]byte(xepgChannel.URL)))
				stream.URL, err = createStreamingURL("DVR", xepgChannel.FileM3UID, xepgChannel.XChannelID, xepgChannel.XName, xepgChannel.URL)
				if err == nil {
					lineup = append(lineup, stream)
				} else {
					ShowError(err, 1202)
				}
			}
		}
	}

	// Sort the lineup
	// Have to use type assertions (https://golang.org/ref/spec#Type_assertions) to cast generic interface{} into LineupStream
	slices.SortFunc(lineup, func(a, b any) int {
		var chanA, chanB float64
		var ok bool
		var lineupA, lineupB LineupStream

		if lineupA, ok = a.(LineupStream); !ok {
			return 0
		}

		if lineupB, ok = b.(LineupStream); !ok {
			return 0
		}

		chanA, _ = strconv.ParseFloat(lineupA.GuideNumber, 64)
		chanB, _ = strconv.ParseFloat(lineupB.GuideNumber, 64)
		if chanA < chanB {
			return -1
		}
		if chanA > chanB {
			return 1
		}
		return 0
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
	newID:
		var ids []string
		id = fmt.Sprintf("id-%d", i)

		for _, v := range Data.Cache.PMS {
			ids = append(ids, v)
		}

		if slices.Contains(ids, id) {
			i++
			goto newID
		}
		return
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
