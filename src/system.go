package src

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// Show Developer Information
func showDevInfo() {
	if System.Dev {
		fmt.Print("\033[31m")
		fmt.Println("* * * * * D E V   M O D E * * * * *")
		fmt.Println("Version: ", System.Version)
		fmt.Println("Build:   ", System.Build)
		fmt.Println("* * * * * * * * * * * * * * * * * *")
		fmt.Print("\033[0m")
		fmt.Println()
	}
}

// Create all System Folders
func createSystemFolders() (err error) {
	e := reflect.ValueOf(&System.Folder).Elem()

	for i := range e.NumField() {
		var folder, ok = e.Field(i).Interface().(string)
		if !ok {
			continue
		}
		err = checkFolder(folder)
		if err != nil {
			return
		}
	}
	return
}

// Create all System Files
func createSystemFiles() (err error) {
	var debug string
	for _, file := range SystemFiles {
		var filename = getPlatformFile(System.Folder.Config + file)

		err = checkFile(filename)
		if err != nil {
			// File does not exist, will be created now
			err = saveMapToJSONFile(filename, make(map[string]any))
			if err != nil {
				return
			}
			debug = fmt.Sprintf("Create File:%s", filename)
			showDebug(debug, 1)
		}

		switch file {
		case "authentication.json":
			System.File.Authentication = filename
		case "pms.json":
			System.File.PMS = filename
		case "settings.json":
			System.File.Settings = filename
		case "xepg.json":
			System.File.XEPG = filename
		case "urls.json":
			System.File.URLS = filename
		}
	}
	return
}

// Load Settings and set Default Values (xTeVe)
func loadSettings() (settings SettingsStruct, err error) {
	settingsMap, err := loadJSONFileToMap(System.File.Settings)
	if err != nil {
		return
	}

	// Set Deafult Values
	var defaults = make(map[string]any)
	var dataMap = make(map[string]any)

	dataMap["xmltv"] = make(map[string]any)
	dataMap["m3u"] = make(map[string]any)
	dataMap["hdhr"] = make(map[string]any)

	defaults["authentication.api"] = false
	defaults["authentication.m3u"] = false
	defaults["authentication.pms"] = false
	defaults["authentication.web"] = false
	defaults["authentication.xml"] = false
	defaults["backup.keep"] = 10
	defaults["backup.path"] = System.Folder.Backup
	defaults["buffer.size.kb"] = 1024
	defaults["buffer.timeout"] = 500
	defaults["buffer.client.timeout"] = 60000
	defaults["buffer"] = "-"
	defaults["cache.images"] = false
	defaults["clearXMLTVCache"] = false
	defaults["defaultMissingEPG"] = "-"
	defaults["disallowURLDuplicates"] = false
	defaults["enableMappedChannels"] = false
	defaults["epgSource"] = "PMS"
	defaults["files.update"] = true
	defaults["files"] = dataMap
	defaults["filter"] = make(map[string]any)
	defaults["hostIP"] = "" // Will be set in resolveHostIP()
	defaults["hostName"] = ""
	defaults["language"] = "en"
	defaults["log.entries.ram"] = 500
	defaults["m3u8.adaptive.bandwidth.mbps"] = 10
	defaults["mapping.first.channel"] = 1000
	defaults["port"] = "34400"
	defaults["ssdp"] = true
	defaults["storeBufferInRAM"] = false
	defaults["temp.path"] = System.Folder.Temp
	defaults["tlsMode"] = false
	defaults["tuner"] = 1
	defaults["udpxy"] = ""
	defaults["update"] = []string{"0000"}
	defaults["user.agent"] = System.Name
	var uuid string
	uuid, err = createUUID()
	if err != nil {
		return
	}
	defaults["uuid"] = uuid
	defaults["version"] = System.DBVersion
	defaults["xepg.replace.missing.images"] = true
	defaults["xteveAutoUpdate"] = true
	defaults["stream.retry.enabled"] = true
	defaults["stream.max.retries"] = 5
	defaults["stream.retry.delay"] = 100

	// Set Default Values
	for key, value := range defaults {
		if _, ok := settingsMap[key]; !ok {
			settingsMap[key] = value
		}
	}

	err = json.Unmarshal([]byte(mapToJSON(settingsMap)), &settings)
	if err != nil {
		return
	}

	// Adopt the settings from the Flags
	if len(System.Flag.Port) > 0 {
		settings.Port = System.Flag.Port
	}

	// Override BufferClientTimeout from environment variable if set
	if envVal := os.Getenv("XTEVE_BUFFER_CLIENT_TIMEOUT"); envVal != "" {
		if val, err := strconv.ParseFloat(envVal, 64); err == nil {
			settings.BufferClientTimeout = val
		}
	}

	// Initialze virutal filesystem for the Buffer
	initBufferVFS(settings.StoreBufferInRAM)

	settings.TempPath = getValidTempDir(settings.TempPath)

	err = saveSettings(settings)
	return
}

// Save Settings (xTeVe)
func saveSettings(settings SettingsStruct) (err error) {
	if settings.BackupKeep == 0 {
		settings.BackupKeep = 10
	}

	if len(settings.BackupPath) == 0 {
		settings.BackupPath = System.Folder.Backup
	}

	if settings.BufferTimeout < 0 {
		settings.BufferTimeout = 0
	}

	if System.Dev {
		Settings.UUID = "2019-01-DEV-xTeVe!"
	}

	System.Folder.Temp = getValidTempDir(settings.TempPath + settings.UUID)

	err = writeByteToFile(System.File.Settings, []byte(mapToJSON(settings)))
	if err != nil {
		return
	}

	Settings = settings

	setDeviceID()
	return
}

// Enable access via the Domain
func setGlobalDomain(domain string) {
	System.Domain = domain

	if Settings.TLSMode {
		System.ServerProtocol.API = "https"
		System.ServerProtocol.DVR = "https"
		System.ServerProtocol.M3U = "https"
		System.ServerProtocol.WEB = "https"
		System.ServerProtocol.XML = "https"
	}

	switch Settings.AuthenticationPMS {
	case true:
		System.Addresses.DVR = "username:password@" + System.Domain
	case false:
		System.Addresses.DVR = System.Domain
	}

	switch Settings.AuthenticationM3U {
	case true:
		System.Addresses.M3U = System.ServerProtocol.M3U + "://" + System.Domain + "/m3u/xteve.m3u?username=xxx&password=yyy<br>(Specific groups: [http://...&group-title=foo,bar])"
	case false:
		System.Addresses.M3U = System.ServerProtocol.M3U + "://" + System.Domain + "/m3u/xteve.m3u     (Specific groups: [http://...?group-title=foo,bar])"
	}

	switch Settings.AuthenticationXML {
	case true:
		System.Addresses.XML = System.ServerProtocol.XML + "://" + System.Domain + "/xmltv/xteve.xml?username=xxx&password=yyy"
	case false:
		System.Addresses.XML = System.ServerProtocol.XML + "://" + System.Domain + "/xmltv/xteve.xml"
	}

	if Settings.EpgSource != "XEPG" {
		System.Addresses.M3U = getErrMsg(2106)
		System.Addresses.XML = getErrMsg(2106)
	}
}

// Generate UUID
func createUUID() (uuid string, err error) {
	var part1, part2 string
	part1, err = randomString(4)
	if err != nil {
		return
	}
	part2, err = randomString(6)
	if err != nil {
		return
	}
	uuid = time.Now().Format("2006-01") + "-" + part1 + "-" + part2
	return
}

// Generate Unique Device ID for Plex
func setDeviceID() {
	var id = Settings.UUID

	switch Settings.Tuner {
	case 1:
		System.DeviceID = id
	default:
		System.DeviceID = fmt.Sprintf("%s:%d", id, Settings.Tuner)
	}
}

// Convert Provider Streaming URL to xTeVe Streaming URL
func createStreamingURL(streamingType, playlistID, channelNumber, channelName, url string) (streamingURL string, err error) {
	var streamInfo StreamInfo
	var serverProtocol string

	if len(Data.Cache.StreamingURLS) == 0 {
		Data.Cache.StreamingURLS = make(map[string]StreamInfo)
	}

	urlID, err := getMD5(fmt.Sprintf("%s-%s", playlistID, url))
	if err != nil {
		return "", err
	}

	if s, ok := Data.Cache.StreamingURLS[urlID]; ok {
		streamInfo = s
	} else {
		streamInfo.URL = url
		streamInfo.Name = channelName
		streamInfo.PlaylistID = playlistID
		streamInfo.ChannelNumber = channelNumber
		streamInfo.URLid = urlID

		Data.Cache.StreamingURLS[urlID] = streamInfo
	}

	switch streamingType {
	case "DVR":
		serverProtocol = System.ServerProtocol.DVR
	case "M3U":
		serverProtocol = System.ServerProtocol.M3U
	}

	streamingURL = fmt.Sprintf("%s://%s/stream/%s", serverProtocol, System.Domain, streamInfo.URLid)
	return
}

func getStreamInfo(urlID string) (streamInfo StreamInfo, err error) {
	if len(Data.Cache.StreamingURLS) == 0 {
		tmp, err := loadJSONFileToMap(System.File.URLS)
		if err != nil {
			return streamInfo, err
		}

		err = json.Unmarshal([]byte(mapToJSON(tmp)), &Data.Cache.StreamingURLS)
		if err != nil {
			return streamInfo, err
		}
	}

	if s, ok := Data.Cache.StreamingURLS[urlID]; ok {
		streamInfo = s
		streamInfo.URL = strings.Trim(streamInfo.URL, "\r\n")
	} else {
		err = errors.New("streaming error")
	}
	return
}
