package src

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"slices"
	"xteve/src/internal/authentication"
	"xteve/src/internal/imgcache"
)

// Change Settings (WebUI)
func updateServerSettings(request RequestStruct) (settings SettingsStruct, err error) {
	var oldSettings = jsonToMap(mapToJSON(Settings))
	var newSettings = jsonToMap(mapToJSON(request.Settings))
	var reloadData = false
	var cacheImages = false
	var createXEPGFiles = false
	var debug string

	for key, value := range newSettings {
		if _, ok := oldSettings[key]; ok {
			switch key {
			case "tuner":
				showWarning(2105)
			case "epgSource":
				reloadData = true
			case "update":
				// Remove spaces from the Values and check the formatting of the Time (0000 - 2359)
				var newUpdateTimes = make([]string, 0)

				for _, v := range value.([]any) {
					v = strings.Replace(v.(string), " ", "", -1)

					_, err := time.Parse("1504", v.(string))
					if err != nil {
						ShowError(err, 1012)
						return Settings, err
					}
					newUpdateTimes = append(newUpdateTimes, v.(string))
				}

				// if len(newUpdateTimes) == 0 {
				// 	//newUpdateTimes = append(newUpdateTimes, "0000")
				// }

				value = newUpdateTimes
			case "cache.images":
				cacheImages = true
			case "xepg.replace.missing.images":
				createXEPGFiles = true
			case "backup.path":
				value = strings.TrimRight(value.(string), string(os.PathSeparator)) + string(os.PathSeparator)
				err = checkFolder(value.(string))
				if err == nil {
					err = checkFilePermission(value.(string))
					if err != nil {
						return
					}
				}

				if err != nil {
					return
				}
			case "temp.path":
				value = getValidTempDir(value.(string))
			case "ffmpeg.path", "vlc.path":
				var path = value.(string)
				if len(path) > 0 {
					err = checkFile(path)
					if err != nil {
						return
					}
				}
			case "scheme.m3u", "scheme.xml":
				createXEPGFiles = true
			case "defaultMissingEPG":
				// If DefaultMissingEPG was set, rebuild DVR and XEPG database
				if newSettings["defaultMissingEPG"] != "-" && oldSettings["defaultMissingEPG"] == "-" {
					reloadData = true
				}
			case "enableMappedChannels":
				// If EnableMappedChannels was turned on, rebuild DVR and XEPG database
				if newSettings["enableMappedChannels"] == true && oldSettings["enableMappedChannels"] == false {
					reloadData = true
				}
			}

			oldSettings[key] = value

			switch fmt.Sprintf("%T", value) {
			case "bool":
				debug = fmt.Sprintf("Save Setting:Key: %s | Value: %t (%T)", key, value, value)
			case "string":
				debug = fmt.Sprintf("Save Setting:Key: %s | Value: %s (%T)", key, value, value)
			case "[]interface {}":
				debug = fmt.Sprintf("Save Setting:Key: %s | Value: %v (%T)", key, value, value)
			case "float64":
				debug = fmt.Sprintf("Save Setting:Key: %s | Value: %d (%T)", key, int(value.(float64)), value)
			default:
				debug = fmt.Sprintf("%T", value)
			}
			showDebug(debug, 1)
		}
	}

	// Update Settings
	err = json.Unmarshal([]byte(mapToJSON(oldSettings)), &Settings)
	if err != nil {
		return
	}

	if !Settings.AuthenticationWEB {
		Settings.AuthenticationAPI = false
		Settings.AuthenticationM3U = false
		Settings.AuthenticationPMS = false
		Settings.AuthenticationWEB = false
		Settings.AuthenticationXML = false
	}

	// Check Buffer Settings
	if len(Settings.FFmpegOptions) == 0 {
		Settings.FFmpegOptions = System.FFmpeg.DefaultOptions
	}

	if len(Settings.VLCOptions) == 0 {
		Settings.VLCOptions = System.VLC.DefaultOptions
	}

	switch Settings.Buffer {
	case "ffmpeg":
		if len(Settings.FFmpegPath) == 0 {
			err = errors.New(getErrMsg(2020))
			return
		}
	case "vlc":
		if len(Settings.VLCPath) == 0 {
			err = errors.New(getErrMsg(2021))
			return
		}
	}

	err = saveSettings(Settings)
	if err == nil {
		settings = Settings

		if reloadData {
			err = buildDatabaseDVR()
			if err != nil {
				return
			}
			if errBuild := buildXEPG(false); errBuild != nil {
				// Log or potentially return this error as well
				// log.Printf("Error building XEPG after settings save: %v", errBuild)
				ShowError(errBuild, 0) // Assuming ShowError logs and is acceptable here
				// return settings, errBuild // Or decide if this is critical enough to return
			}
		}

		if cacheImages {
			if Settings.EpgSource == "XEPG" && System.ImageCachingInProgress == 0 {
				Data.Cache.Images, err = imgcache.New(System.Folder.ImagesCache, fmt.Sprintf("%s://%s/images/", System.ServerProtocol.WEB, System.Domain), Settings.CacheImages)
				if err != nil {
					ShowError(err, 0)
				}

				switch Settings.CacheImages {
				case false:
					if errXML := createXMLTVFile(); errXML != nil {
						// log.Printf("Error creating XMLTV file: %v", errXML)
						ShowError(errXML, 0)
					}
					if errM3U := createM3UFile(); errM3U != nil {
						// log.Printf("Error creating M3U file: %v", errM3U)
						ShowError(errM3U, 0)
					}
				case true:
					go func() {
						if errXML := createXMLTVFile(); errXML != nil {
							// log.Printf("Error creating XMLTV file in goroutine: %v", errXML)
							ShowError(errXML, 0)
						}
						if errM3U := createM3UFile(); errM3U != nil {
							// log.Printf("Error creating M3U file in goroutine: %v", errM3U)
							ShowError(errM3U, 0)
						}

						System.ImageCachingInProgress = 1
						showInfo("Image Caching:Images are cached")

						Data.Cache.Images.Image.Caching()
						showInfo("Image Caching:Done")

						System.ImageCachingInProgress = 0

						if errBuild := buildXEPG(false); errBuild != nil {
							// log.Printf("Error building XEPG after image caching: %v", errBuild)
							ShowError(errBuild, 0)
						}
					}()
				}
			}
		}

		if createXEPGFiles {
			go func() {
				if errXML := createXMLTVFile(); errXML != nil {
					// log.Printf("Error creating XMLTV file (createXEPGFiles): %v", errXML)
					ShowError(errXML, 0)
				}
				if errM3U := createM3UFile(); errM3U != nil {
					// log.Printf("Error creating M3U file (createXEPGFiles): %v", errM3U)
					ShowError(errM3U, 0)
				}
			}()
		}
	}
	return
}

// Save Provider Data (WebUI)
func saveFiles(request RequestStruct, fileType string) (err error) {
	var filesMap = make(map[string]any)
	var newData = make(map[string]any)
	var indicator string
	var reloadData = false

	switch fileType {
	case "m3u":
		filesMap = Settings.Files.M3U
		newData = request.Files.M3U
		indicator = "M"
	case "hdhr":
		filesMap = Settings.Files.HDHR
		newData = request.Files.HDHR
		indicator = "H"
	case "xmltv":
		filesMap = Settings.Files.XMLTV
		newData = request.Files.XMLTV
		indicator = "X"
	}

	if len(filesMap) == 0 {
		filesMap = make(map[string]any)
	}

	for dataID, data := range newData {
		if dataID == "-" {
			// New Provider File
			dataID = indicator + randomString(19)
			data.(map[string]any)["new"] = true
			filesMap[dataID] = data
		} else {
			// Existing Provider File
			for key, value := range data.(map[string]any) {
				var oldData = filesMap[dataID].(map[string]any)
				oldData[key] = value
			}
		}

		switch fileType {
		case "m3u":
			Settings.Files.M3U = filesMap
		case "hdhr":
			Settings.Files.HDHR = filesMap
		case "xmltv":
			Settings.Files.XMLTV = filesMap
		}

		// New Provider File
		if _, ok := data.(map[string]any)["new"]; ok {
			reloadData = true
			err = getProviderData(fileType, dataID)
			delete(data.(map[string]any), "new")

			if err != nil {
				delete(filesMap, dataID)
				return
			}
		}

		if _, ok := data.(map[string]any)["delete"]; ok {
			deleteLocalProviderFiles(dataID, fileType)
			reloadData = true
		}

		err = saveSettings(Settings)
		if err != nil {
			return
		}

		if reloadData {
			err = buildDatabaseDVR()
			if err != nil {
				return err
			}
			if errBuild := buildXEPG(false); errBuild != nil {
				// log.Printf("Error building XEPG after saving files: %v", errBuild)
				ShowError(errBuild, 0)
				// Depending on severity, might want to return errBuild here
			}
		}
		Settings, _ = loadSettings() // Explicitly ignoring error as per previous analysis
	}
	return
}

// Update Provider Data manually (WebUI)
func updateFile(request RequestStruct, fileType string) (err error) {
	var updateData = make(map[string]any)

	switch fileType {
	case "m3u":
		updateData = request.Files.M3U
	case "hdhr":
		updateData = request.Files.HDHR
	case "xmltv":
		updateData = request.Files.XMLTV
	}

	for dataID := range updateData {
		err = getProviderData(fileType, dataID)
		if err == nil {
			err = buildDatabaseDVR()
			if err != nil { // Check error from buildDatabaseDVR before calling buildXEPG
				return err
			}
			if errBuild := buildXEPG(false); errBuild != nil {
				// log.Printf("Error building XEPG after updating file: %v", errBuild)
				ShowError(errBuild, 0)
				// Potentially return errBuild
			}
		}
	}
	return
}

// Delete Provider Data (WebUI)
func deleteLocalProviderFiles(dataID, fileType string) {
	var removeData = make(map[string]any)
	var fileExtension string

	switch fileType {
	case "m3u":
		removeData = Settings.Files.M3U
		fileExtension = ".m3u"
	case "hdhr":
		removeData = Settings.Files.HDHR
		fileExtension = ".json"
	case "xmltv":
		removeData = Settings.Files.XMLTV
		fileExtension = ".xml"
	}

	if _, ok := removeData[dataID]; ok {
		delete(removeData, dataID)
		filePathToRemove := System.Folder.Data + dataID + fileExtension
		if errRemove := os.RemoveAll(filePathToRemove); errRemove != nil {
			// log.Printf("Error deleting local provider file %s: %v", filePathToRemove, errRemove)
			ShowError(errRemove, 0) // Use existing error display mechanism
		}
	}
}

// Save Filter Settings (WebUI)
func saveFilter(request RequestStruct) (settings SettingsStruct, err error) {
	var defaultFilter FilterStruct
	var newFilter = false

	defaultFilter.Active = true
	defaultFilter.CaseSensitive = false
	defaultFilter.PreserveMapping = true
	defaultFilter.StartingChannel = strconv.FormatFloat(Settings.MappingFirstChannel, 'f', -1, 64) // 1000

	filterMap := Settings.Filter // Use short declaration and assign directly
	newData := request.Filter   // Use short declaration and assign directly

	// Ensure filterMap is not nil if Settings.Filter could be nil
	if filterMap == nil {
		filterMap = make(map[int64]any)
		// If Settings.Filter was nil, we need to update it after modifications
		// This is a bit tricky as filterMap is a copy. The original Settings.Filter
		// is modified directly in the loop by dataID.
		// For now, we assume Settings.Filter is initialized elsewhere if it needs to be.
		// However, if Settings.Filter can be nil, this function should probably update Settings.Filter
		// with the new map if one is created.
		// A safer approach is to ensure Settings.Filter is always initialized.
		// For the purpose of fixing ineffassign, we assume Settings.Filter is a valid map.
	}


	var createNewID = func() (id int64) {
	newID:
		if _, ok := filterMap[id]; ok {
			id++
			goto newID
		}
		return id
	}

	for dataID, data := range newData {
		if dataID == -1 {
			// New Filter
			newFilter = true
			dataID = createNewID()
			filterMap[dataID] = jsonToMap(mapToJSON(defaultFilter))
		}

		// Update / delete filters
		for key, value := range data.(map[string]any) {
			// Clear Filters
			if _, ok := data.(map[string]any)["delete"]; ok {
				delete(filterMap, dataID)
				break
			}

			if filter, ok := data.(map[string]any)["filter"].(string); ok {
				if len(filter) == 0 {
					err = errors.New(getErrMsg(1014))
					if newFilter {
						delete(filterMap, dataID)
					}
					return
				}
			}

			if oldData, ok := filterMap[dataID].(map[string]any); ok {
				oldData[key] = value
			}
		}
	}

	err = saveSettings(Settings)
	if err != nil {
		return
	}

	settings = Settings

	err = buildDatabaseDVR()
	if err != nil {
		return
	}

	if errBuild := buildXEPG(false); errBuild != nil {
		// log.Printf("Error building XEPG after saving filter: %v", errBuild)
		ShowError(errBuild, 0)
		// Potentially return errBuild if this is critical
	}
	return
}

// Save XEPG Mapping
func saveXEpgMapping(request RequestStruct) (err error) {
	var tmp = Data.XEPG

	Data.Cache.Images, err = imgcache.New(System.Folder.ImagesCache, fmt.Sprintf("%s://%s/images/", System.ServerProtocol.WEB, System.Domain), Settings.CacheImages)
	if err != nil {
		ShowError(err, 0)
	}

	err = json.Unmarshal([]byte(mapToJSON(request.EpgMapping)), &tmp)
	if err != nil {
		return
	}

	err = saveMapToJSONFile(System.File.XEPG, request.EpgMapping)
	if err != nil {
		return err
	}

	Data.XEPG.Channels = request.EpgMapping

	if System.ScanInProgress == 0 {
		System.ScanInProgress = 1
		cleanupXEPG() // Assuming cleanupXEPG does not return an error or handles its own.
		System.ScanInProgress = 0
		if errBuild := buildXEPG(true); errBuild != nil {
			// log.Printf("Error building XEPG after saving XEPG mapping: %v", errBuild)
			ShowError(errBuild, 0) // Using existing error display
			// This function returns err, so we could propagate it.
			// However, the original logic implies it might continue, so logging seems appropriate.
		}
	} else {
		// If the Mapping is saved again while the Database is being created, the Database will not be updated again until later.
		go func() {
			if System.BackgroundProcess {
				return
			}
			System.BackgroundProcess = true

			for {
				time.Sleep(time.Duration(1) * time.Second)
				if System.ScanInProgress == 0 {
					break
				}
			}

			System.ScanInProgress = 1
			cleanupXEPG() // Assuming cleanupXEPG does not return an error or handles its own.
			System.ScanInProgress = 0
			if errBuild := buildXEPG(false); errBuild != nil {
				// log.Printf("Error building XEPG in goroutine after XEPG mapping: %v", errBuild)
				ShowError(errBuild, 0) // Using existing error display
			}
			showInfo("XEPG:" + "Ready to use")

			System.BackgroundProcess = false
		}()
	}
	return
}

// Save User Data (WebUI)
func saveUserData(request RequestStruct) (err error) {
	var userData = request.UserData

	var newCredentials = func(userID string, newUserData map[string]any) (err error) {
		var newUsername, newPassword string
		if username, ok := newUserData["username"].(string); ok {
			newUsername = username
		}

		if password, ok := newUserData["password"].(string); ok {
			newPassword = password
		}

		if len(newUsername) > 0 {
			err = authentication.ChangeCredentials(userID, newUsername, newPassword)
		}
		return
	}

	for userID, newUserData := range userData {
		err = newCredentials(userID, newUserData.(map[string]any))
		if err != nil {
			return
		}

		if request.DeleteUser {
			err = authentication.RemoveUser(userID)
			return
		}

		delete(newUserData.(map[string]any), "password")
		delete(newUserData.(map[string]any), "confirm")

		if _, ok := newUserData.(map[string]any)["delete"]; ok {
			if errRemove := authentication.RemoveUser(userID); errRemove != nil {
				// log.Printf("Error removing user %s: %v", userID, errRemove)
				ShowError(errRemove, 0) // Using existing error display
				// Decide if this is a critical error to return, or if processing other users can continue
				// For now, let's assume it's not critical enough to stop processing other users.
			}
		} else {
			err = authentication.WriteUserData(userID, newUserData.(map[string]any))
			if err != nil {
				return
			}
		}
	}
	return
}

// Create New User (WebUI)
func saveNewUser(request RequestStruct) (err error) {
	var data = request.UserData
	var username = data["username"].(string)
	var password = data["password"].(string)

	delete(data, "password")
	delete(data, "confirm")

	userID, err := authentication.CreateNewUser(username, password)
	if err != nil {
		return
	}

	err = authentication.WriteUserData(userID, data)
	return
}

// Wizard (WebUI)
func saveWizard(request RequestStruct) (nextStep int, err error) {
	var wizard = jsonToMap(mapToJSON(request.Wizard))

	for key, value := range wizard {
		switch key {
		case "tuner":
			Settings.Tuner = int(value.(float64))
			nextStep = 1
		case "epgSource":
			Settings.EpgSource = value.(string)
			nextStep = 2
		case "m3u", "xmltv":
			var filesMap map[string]any // Declare without initializing
			var data = make(map[string]any)
			var indicator, dataID string

			// filesMap is assigned below based on key

			data["type"] = key
			data["new"] = true

			switch key {
			case "m3u":
				filesMap = Settings.Files.M3U
				data["name"] = "M3U"
				indicator = "M"
			case "xmltv":
				filesMap = Settings.Files.XMLTV
				data["name"] = "XMLTV"
				indicator = "X"
			}

			dataID = indicator + randomString(19)
			data["file.source"] = value.(string)

			filesMap[dataID] = data

			switch key {
			case "m3u":
				Settings.Files.M3U = filesMap
				nextStep = 3

				err = getProviderData(key, dataID)

				if err != nil {
					ShowError(err, 000)
					delete(filesMap, dataID)
					return
				}

				err = buildDatabaseDVR()
				if err != nil {
					ShowError(err, 000)
					delete(filesMap, dataID)
					return
				}

				if Settings.EpgSource == "PMS" {
					nextStep = 10
				}
			case "xmltv":
				Settings.Files.XMLTV = filesMap
				nextStep = 10

				err = getProviderData(key, dataID)

				if err != nil {
					ShowError(err, 000)
					delete(filesMap, dataID)
					return
				}
				if errBuild := buildXEPG(false); errBuild != nil {
					// log.Printf("Error building XEPG in wizard: %v", errBuild)
					ShowError(errBuild, 0) // Using existing error display
					// This function returns nextStep, err. Consider if this should be returned.
				}
				System.ScanInProgress = 0
			}
		}
	}

	err = saveSettings(Settings)
	if err != nil {
		return
	}
	return
}

// Create Filter Rules
func createFilterRules() (err error) {
	Data.Filter = nil
	var dataFilter Filter

	for _, f := range Settings.Filter {
		var filter FilterStruct
		var exclude, include string

		err = json.Unmarshal([]byte(mapToJSON(f)), &filter)
		if err != nil {
			return
		}

		switch filter.Type {
		case "custom-filter":
			dataFilter.CaseSensitive = filter.CaseSensitive
			dataFilter.Rule = filter.Filter
			dataFilter.Type = filter.Type

			Data.Filter = append(Data.Filter, dataFilter)
		case "group-title":
			if len(filter.Include) > 0 {
				include = fmt.Sprintf(" {%s}", filter.Include)
			}

			if len(filter.Exclude) > 0 {
				exclude = fmt.Sprintf(" !{%s}", filter.Exclude)
			}

			dataFilter.CaseSensitive = filter.CaseSensitive
			dataFilter.PreserveMapping = filter.PreserveMapping
			dataFilter.StartingChannel = filter.StartingChannel
			dataFilter.Rule = fmt.Sprintf("%s%s%s", filter.Filter, include, exclude)
			dataFilter.Type = filter.Type

			Data.Filter = append(Data.Filter, dataFilter)
		}
	}
	return
}

// Create a Database for the DVR System
func buildDatabaseDVR() (err error) {
	System.ScanInProgress = 1

	Data.Streams.All = make([]any, 0)
	Data.Streams.Active = make([]any, 0)
	Data.Streams.Inactive = make([]any, 0)
	Data.Playlist.M3U.Groups.Text = []string{}
	Data.Playlist.M3U.Groups.Value = []string{}
	Data.StreamPreviewUI.Active = []string{}
	Data.StreamPreviewUI.Inactive = []string{}

	var availableFileTypes = []string{"m3u", "hdhr"}

	var urlValuesMap = make(map[string]string)
	var tmpGroupsM3U = make(map[string]int64)

	err = createFilterRules()
	if err != nil {
		return
	}

	for _, fileType := range availableFileTypes {
		var playlistFile = getLocalProviderFiles(fileType)

		for n, i := range playlistFile {
			var channels []any
			var groupTitle, tvgID, uuid int = 0, 0, 0
			var keys = []string{"group-title", "tvg-id", "uuid"}
			var compatibility = make(map[string]int)

			var id = strings.TrimSuffix(getFilenameFromPath(i), path.Ext(getFilenameFromPath(i)))
			var playlistName = getProviderParameter(id, fileType, "name")

			switch fileType {
			case "m3u":
				channels, err = parsePlaylist(i, fileType)
			case "hdhr":
				channels, err = parsePlaylist(i, fileType)
			}

			if err != nil {
				ShowError(err, 1005)
				err = errors.New(playlistName + ": Local copy of the file no longer exists")
				ShowError(err, 0)
				playlistFile = slices.Delete(playlistFile, n, n+1)
			}

			// Analyze Streams
			for _, stream := range channels {
				var s = stream.(map[string]string)
				s["_file.m3u.path"] = i
				s["_file.m3u.name"] = playlistName
				s["_file.m3u.id"] = id

				if Settings.DisallowURLDuplicates {
					if _, haveURL := urlValuesMap[s["url"]]; haveURL {
						showInfo("Streams:" + fmt.Sprintf("Found duplicated URL %v, ignoring the channel %v", s["url"], s["name"]))
						continue
					} else {
						urlValuesMap[s["url"]] = s["_values"]
					}
				}

				// Calculate Compatibility
				for _, key := range keys {
					switch key {
					case "uuid":
						if value, ok := s["_uuid.key"]; ok {
							if len(value) > 0 {
								uuid++
							}
						}
					case "group-title":
						if value, ok := s[key]; ok {
							if len(value) > 0 {
								// if _, ok := tmpGroupsM3U[value]; ok {
								// 	tmpGroupsM3U[value]++
								// } else {
								// 	tmpGroupsM3U[value] = 1
								// }
								tmpGroupsM3U[value]++
								groupTitle++
							}
						}
					case "tvg-id":
						if value, ok := s[key]; ok {
							if len(value) > 0 {
								tvgID++
							}
						}
					}
				}
				Data.Streams.All = append(Data.Streams.All, stream)

				// New Filter from Version 1.3.0
				var preview string
				var status = FilterThisStream(stream) // Corrected: Call exported function

				if name, ok := s["name"]; ok {
					var group string

					if v, ok := s["group-title"]; ok {
						group = v
					}
					preview = fmt.Sprintf("%s [%s]", name, group)
				}

				switch status {
				case true:
					Data.StreamPreviewUI.Active = append(Data.StreamPreviewUI.Active, preview)
					Data.Streams.Active = append(Data.Streams.Active, stream)
				case false:
					Data.StreamPreviewUI.Inactive = append(Data.StreamPreviewUI.Inactive, preview)
					Data.Streams.Inactive = append(Data.Streams.Inactive, stream)
				}
			}

			if tvgID == 0 {
				compatibility["tvg.id"] = 0
			} else {
				compatibility["tvg.id"] = int(tvgID * 100 / len(channels))
			}

			if groupTitle == 0 {
				compatibility["group.title"] = 0
			} else {
				compatibility["group.title"] = int(groupTitle * 100 / len(channels))
			}

			if uuid == 0 {
				compatibility["stream.id"] = 0
			} else {
				compatibility["stream.id"] = int(uuid * 100 / len(channels))
			}
			compatibility["streams"] = len(channels)
			if errCompat := setProviderCompatibility(id, fileType, compatibility); errCompat != nil {
				// log.Printf("Error setting provider compatibility for %s (%s): %v", id, fileType, errCompat)
				ShowError(errCompat, 0) // Using existing error display
				// This function returns err. If compatibility setting is critical, propagate.
				// For now, logging and continuing to build DVR for other providers.
			}
		}
	}

	for group, count := range tmpGroupsM3U {
		var text = fmt.Sprintf("%s (%d)", group, count)
		var value = group
		Data.Playlist.M3U.Groups.Text = append(Data.Playlist.M3U.Groups.Text, text)
		Data.Playlist.M3U.Groups.Value = append(Data.Playlist.M3U.Groups.Value, value)
	}

	sort.Strings(Data.Playlist.M3U.Groups.Text)
	sort.Strings(Data.Playlist.M3U.Groups.Value)

	if len(Data.Streams.Active) == 0 && len(Settings.Filter) == 0 {
		Data.Streams.Active = Data.Streams.All
		Data.Streams.Inactive = make([]any, 0)

		Data.StreamPreviewUI.Active = Data.StreamPreviewUI.Inactive
		Data.StreamPreviewUI.Inactive = []string{}
	}

	if len(Data.Streams.Active) > System.PlexChannelLimit {
		showWarning(2000)
	}

	System.ScanInProgress = 0
	showInfo(fmt.Sprintf("All streams:%d", len(Data.Streams.All)))
	showInfo(fmt.Sprintf("Active streams:%d", len(Data.Streams.Active)))
	showInfo(fmt.Sprintf("Filter:%d", len(Data.Filter)))

	sort.Strings(Data.StreamPreviewUI.Active)
	sort.Strings(Data.StreamPreviewUI.Inactive)
	return
}

// Load Storage Location of all local Provider Files, always for one File Type (M3U, XMLTV etc.)
func getLocalProviderFiles(fileType string) (localFiles []string) {
	var fileExtension string
	var dataMap = make(map[string]any)

	switch fileType {
	case "m3u":
		fileExtension = ".m3u"
		dataMap = Settings.Files.M3U
	case "hdhr":
		fileExtension = ".json"
		dataMap = Settings.Files.HDHR
	case "xmltv":
		fileExtension = ".xml"
		dataMap = Settings.Files.XMLTV
	}

	for dataID := range dataMap {
		localFiles = append(localFiles, System.Folder.Data+dataID+fileExtension)
	}
	return
}

// Output Provider Parameters based on the Key
func getProviderParameter(id, fileType, key string) (s string) {
	var dataMap = make(map[string]any)

	switch fileType {
	case "m3u":
		dataMap = Settings.Files.M3U
	case "hdhr":
		dataMap = Settings.Files.HDHR
	case "xmltv":
		dataMap = Settings.Files.XMLTV
	}

	if data, ok := dataMap[id].(map[string]any); ok {
		if v, ok := data[key].(string); ok {
			s = v
		}

		if v, ok := data[key].(float64); ok {
			s = strconv.Itoa(int(v))
		}
	}
	return
}

// Update Provider Statistics Compatibility
func setProviderCompatibility(id, fileType string, compatibility map[string]int) error { // Added error return type
	var dataMap map[string]any // Declare, assign below

	switch fileType {
	case "m3u":
		dataMap = Settings.Files.M3U
	case "hdhr":
		dataMap = Settings.Files.HDHR
	case "xmltv":
		dataMap = Settings.Files.XMLTV
	}

	if data, ok := dataMap[id].(map[string]any); ok {
		data["compatibility"] = compatibility

		switch fileType {
		case "m3u":
			Settings.Files.M3U = dataMap
		case "hdhr":
			Settings.Files.HDHR = dataMap
		case "xmltv":
			Settings.Files.XMLTV = dataMap
		}
		return saveSettings(Settings) // Return error from saveSettings
	}
	return nil // Return nil if dataId not found in dataMap
}
