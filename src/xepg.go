package src

import (
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path"
	"regexp"
	"runtime"
	"slices"

	"crypto/md5"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"xteve/src/internal/imgcache"
)

// Check provider XMLTV File
func checkXMLCompatibility(id string, body []byte) (err error) {
	var xmltv XMLTV
	var compatibility = make(map[string]int)

	err = xml.Unmarshal(body, &xmltv)
	if err != nil {
		return
	}

	compatibility["xmltv.channels"] = len(xmltv.Channel)
	compatibility["xmltv.programs"] = len(xmltv.Program)

	err = setProviderCompatibility(id, "xmltv", compatibility)
	return
}

// Create XEPG Data
func buildXEPG(background bool) error { // Added error return type
	if System.ScanInProgress == 1 {
		return nil // Or an error indicating it's busy
	}
	System.ScanInProgress = 1
	var err error // Keep for local error handling before returning

	Data.Cache.Images, err = imgcache.New(System.Folder.ImagesCache, fmt.Sprintf("%s://%s/images/", System.ServerProtocol.WEB, System.Domain), Settings.CacheImages)
	if err != nil {
		ShowError(err, 0)
		// Decide if this is fatal for buildXEPG
		// For now, let's assume it can continue and might only affect images
	}

	if Settings.EpgSource == "XEPG" {
		if background { // Was: case true
			go func() {
				// Functions in goroutine, their errors can't be directly returned by buildXEPG
				createXEPGMapping()
				if dbErr := createXEPGDatabase(); dbErr != nil {
					ShowError(fmt.Errorf("error creating XEPG database in background: %w", dbErr), 0)
				}
				if mapErr := mapping(); mapErr != nil {
					ShowError(fmt.Errorf("error during XEPG mapping in background: %w", mapErr), 0)
				}
				cleanupXEPG()
				if xmlErr := createXMLTVFile(); xmlErr != nil {
					ShowError(fmt.Errorf("error creating XMLTV file in background: %w", xmlErr), 0)
				}
				if m3uErr := createM3UFile(); m3uErr != nil {
					ShowError(fmt.Errorf("error creating M3U file in background: %w", m3uErr), 0)
				}

				showInfo("XEPG:" + "Ready to use")

				if Settings.CacheImages && System.ImageCachingInProgress == 0 {
					go func() {
						System.ImageCachingInProgress = 1
						showInfo(fmt.Sprintf("Image Caching:Images are cached (%d)", len(Data.Cache.Images.Queue)))
						Data.Cache.Images.Image.Caching()
						Data.Cache.Images.Image.Remove()
						showInfo("Image Caching:Done")
						if err := createXMLTVFile(); err != nil {
							ShowError(err, 0)
						}
						if err := createM3UFile(); err != nil {
							ShowError(err, 0)
						}
						System.ImageCachingInProgress = 0
					}()
				}
				System.ScanInProgress = 0
				if Settings.ClearXMLTVCache {
					clearXMLTVCache()
				}
			}()
			return nil // Background task launched, main function returns no immediate error
		} else { // Was: case false
			createXEPGMapping() // Assuming this doesn't return error or handles internally
			if err = createXEPGDatabase(); err != nil {
				ShowError(fmt.Errorf("error creating XEPG database: %w", err), 0)
				System.ScanInProgress = 0
				return err
			}
			if err = mapping(); err != nil {
				ShowError(fmt.Errorf("error during XEPG mapping: %w", err), 0)
				System.ScanInProgress = 0
				return err
			}
			cleanupXEPG()

			// Create files synchronously when not in background mode
			if err := createXMLTVFile(); err != nil {
				ShowError(fmt.Errorf("error creating XMLTV file: %w", err), 0)
				// Even with an error, we might want to try creating the M3U file
			}
			if err := createM3UFile(); err != nil {
				ShowError(fmt.Errorf("error creating M3U file: %w", err), 0)
				System.ScanInProgress = 0
				return err // M3U file is critical for clients, so maybe return error
			}

			if Settings.CacheImages && System.ImageCachingInProgress == 0 {
				// Run caching in the background as it can be slow
				go func() {
					System.ImageCachingInProgress = 1
					showInfo(fmt.Sprintf("Image Caching:Images are cached (%d)", len(Data.Cache.Images.Queue)))
					Data.Cache.Images.Image.Caching()
					Data.Cache.Images.Image.Remove()
					showInfo("Image Caching:Done")
					// After caching, regenerate files to update image URLs
					if xmlErr := createXMLTVFile(); xmlErr != nil {
						ShowError(fmt.Errorf("error creating XMLTV file post-cache: %w", xmlErr), 0)
					}
					if m3uErr := createM3UFile(); m3uErr != nil {
						ShowError(fmt.Errorf("error creating M3U file post-cache: %w", m3uErr), 0)
					}
					System.ImageCachingInProgress = 0
				}()
			}

			showInfo("XEPG:" + "Ready to use")
			System.ScanInProgress = 0
			if Settings.ClearXMLTVCache {
				clearXMLTVCache()
			}
			return nil
		}
	} else {
		// getLineup() // Assuming getLineup() modifies globals and doesn't return error, or handles its own.
		// If getLineup can fail and that failure should be propagated, it needs to return error.
		// For now, assume it matches original behavior.
		if _, err := getLineup(); err != nil {
			ShowError(err, 0)
		}
		System.ScanInProgress = 0
		return nil
	}
}

// Create Mapping Menu for the XMLTV Files
func createXEPGMapping() {
	Data.XMLTV.Files = getLocalProviderFiles("xmltv")
	Data.XMLTV.Mapping = make(map[string]any)

	var tmpMap = make(map[string]any)

	if len(Data.XMLTV.Files) > 0 {
		for i := len(Data.XMLTV.Files) - 1; i >= 0; i-- {
			var file = Data.XMLTV.Files[i]
			var err error
			var fileID = strings.TrimSuffix(getFilenameFromPath(file), path.Ext(getFilenameFromPath(file)))
			showInfo("XEPG:" + "Parse XMLTV file: " + getProviderParameter(fileID, "xmltv", "name"))

			var xmltv XMLTV

			err = getLocalXMLTV(file, &xmltv)
			if err != nil {
				Data.XMLTV.Files = append(Data.XMLTV.Files, Data.XMLTV.Files[i+1:]...)
				var errMsg = err.Error()
				err = errors.New(getProviderParameter(fileID, "xmltv", "name") + ": " + errMsg)
				ShowError(err, 000)
			}

			// XML Parsing (Provider File)
			if err == nil {
				// Write Data from the XML File to a temporary Map
				var xmltvMap = make(map[string]any)

				for _, c := range xmltv.Channel {
					var channel = make(map[string]any)
					channel["id"] = c.ID
					channel["display-names"] = c.DisplayNames
					channel["icon"] = c.Icon.Src
					xmltvMap[c.ID] = channel
				}
				tmpMap[getFilenameFromPath(file)] = xmltvMap
				Data.XMLTV.Mapping[getFilenameFromPath(file)] = xmltvMap
			}
		}
		Data.XMLTV.Mapping = tmpMap
	} else {
		if !System.ConfigurationWizard {
			showWarning(1007)
		}
	}

	// Create selection for the Dummy
	var dummy = make(map[string]any)
	var times = []string{"30", "60", "90", "120", "180", "240", "360"}

	for _, i := range times {
		var dummyChannel = make(map[string]any)
		dummyChannel["display-names"] = []DisplayName{{Value: i + " Minutes"}}
		dummyChannel["id"] = i + "_Minutes"
		dummyChannel["icon"] = ""
		if id, ok := dummyChannel["id"].(string); ok {
			dummy[id] = dummyChannel
		}
	}
	Data.XMLTV.Mapping["xTeVe Dummy"] = dummy
}

// Create / update XEPG Database
func createXEPGDatabase() (err error) {
	var allChannelNumbers = make([]float64, 0)
	Data.Cache.Streams.Active = make([]string, 0)
	Data.XEPG.Channels = make(map[string]XEPGChannelStruct)

	Data.XEPG.Channels, err = loadXEPGChannels(System.File.XEPG)
	if err != nil {
		ShowError(err, 1004)
		return err
	}

	showInfo("XEPG:" + "Update database")

	// Delete Channel with missing Channel Numbers.
	for id, xepgChannel := range Data.XEPG.Channels {
		if len(xepgChannel.XChannelID) == 0 {
			delete(Data.XEPG.Channels, id)
		}

		if xChannelID, err := strconv.ParseFloat(xepgChannel.XChannelID, 64); err == nil {
			allChannelNumbers = append(allChannelNumbers, xChannelID)
		}
	}

	// Make a map of the db channels based on their previously downloaded attributes -- filename, group, title, etc
	var xepgChannelsValuesMap = make(map[string]XEPGChannelStruct)
	for _, channel := range Data.XEPG.Channels {
		if len(channel.UpdateChannelNameRegex) > 0 {
			channel.CompiledNameRegex, err = regexp.Compile(channel.UpdateChannelNameRegex)
			if err != nil {
				ShowError(err, 1018)
			}
		}

		if len(channel.UpdateChannelNameByGroupRegex) > 0 {
			channel.CompiledGroupRegex, err = regexp.Compile(channel.UpdateChannelNameByGroupRegex)
			if err != nil {
				ShowError(err, 1018)
			}
		}
		channelHash := generateChannelHash(channel.FileM3UID, channel.Name, channel.GroupTitle, channel.TvgID, channel.TvgName, channel.UUIDKey, channel.UUIDValue)
		xepgChannelsValuesMap[channelHash] = channel
	}

	for _, dsa := range Data.Streams.Active {
		var channelExists = false  // Decides whether a Channel should be added to the Database
		var channelHasUUID = false // Checks whether the Channel (Stream) has Unique IDs
		var currentXEPGID string   // Current Database ID (XEPG) Used to update the Channel in the Database with the Stream of the M3U

		var m3uChannel M3UChannelStructXEPG

		// Optimization: Manual assignment for map[string]string avoids JSON overhead
		if streamMap, ok := dsa.(map[string]string); ok {
			bindMapToM3UChannelStruct(streamMap, &m3uChannel)
		} else {
			// Fallback for other types
			err = bindToStruct(dsa, &m3uChannel)
			if err != nil {
				return
			}
		}

		Data.Cache.Streams.Active = append(Data.Cache.Streams.Active, m3uChannel.Name+m3uChannel.FileM3UID)

		// Try to find the channel based on matching all known values. If that fails, then move to full channel scan
		m3uChannelHash := generateChannelHash(m3uChannel.FileM3UID, m3uChannel.Name, m3uChannel.GroupTitle, m3uChannel.TvgID, m3uChannel.TvgName, m3uChannel.UUIDKey, m3uChannel.UUIDValue)
		if val, ok := xepgChannelsValuesMap[m3uChannelHash]; ok {
			channelExists = true
			currentXEPGID = val.XEPG
			if len(m3uChannel.UUIDValue) > 0 {
				channelHasUUID = true
			}
		} else {
			// Run through the XEPG Database to search for the Channel (full scan)
			for _, dxc := range xepgChannelsValuesMap {
				if m3uChannel.FileM3UID == dxc.FileM3UID {
					dxc.FileM3UID = m3uChannel.FileM3UID
					dxc.FileM3UName = m3uChannel.FileM3UName

					// Compare the Stream using a UUID in the M3U with the Channel in the Database
					if len(dxc.UUIDValue) > 0 && len(m3uChannel.UUIDValue) > 0 {
						if dxc.UUIDValue == m3uChannel.UUIDValue && dxc.UUIDKey == m3uChannel.UUIDKey {
							channelExists = true
							channelHasUUID = true
							currentXEPGID = dxc.XEPG
							break
						}
					} else {
						// Compare the Stream to the Channel in the Database using the Channel Name
						if dxc.Name == m3uChannel.Name {
							channelExists = true
							currentXEPGID = dxc.XEPG
							break
						}
					}

					// Rename the Channel if it's update regex matches new channel name
					if len(dxc.UpdateChannelNameRegex) == 0 {
						continue
					}
					// Guard against the situation when both channels have UUIDValue, they are different, but names are the same
					if dxc.Name == m3uChannel.Name {
						continue
					}

					if dxc.CompiledNameRegex == nil {
						continue
					}

					if !dxc.CompiledNameRegex.MatchString(m3uChannel.Name) {
						continue
					}

					if dxc.CompiledGroupRegex != nil {
						if !dxc.CompiledGroupRegex.MatchString(dxc.XGroupTitle) {
							// Found the channel name to update but it has wrong group
							continue
						}
					}
					showInfo("XEPG:" + fmt.Sprintf("Renaming the channel '%v' to '%v'", dxc.Name, m3uChannel.Name))
					channelExists = true
					// dxc.Name will be assigned later in channelExists switch
					dxc.XName = m3uChannel.Name
					currentXEPGID = dxc.XEPG
					Data.XEPG.Channels[currentXEPGID] = dxc
					break
				}
			}
		}

		if channelExists { // Was: case true
			// Existing Channel
			err = processExistingXEPGChannel(m3uChannel, currentXEPGID, channelHasUUID)
			if err != nil {
				return
			}
		} else { // Was: case false
			// New Channel
			processNewXEPGChannel(m3uChannel, &allChannelNumbers)
		}
	}
	showInfo("XEPG:" + "Save DB file")
	err = saveMapToJSONFile(System.File.XEPG, Data.XEPG.Channels)
	if err != nil {
		return
	}
	return
}

// generateNewXEPGID creates a new unique XEPG ID.
func generateNewXEPGID() (xepgID string) {
	var firstID = 0
	for {
		xepgID = "x-ID." + strconv.FormatInt(int64(firstID), 10)
		if _, ok := Data.XEPG.Channels[xepgID]; !ok {
			return xepgID // Found a free ID
		}
		firstID++
	}
}

// findFreeChannelNumber finds the next available channel number.
func findFreeChannelNumber(allChannelNumbers *[]float64, startingChannel ...string) (xChannelID string) {
	slices.Sort(*allChannelNumbers)

	var firstFreeNumber float64 = Settings.MappingFirstChannel
	if len(startingChannel) > 0 && startingChannel[0] != "" {
		var startNum, err = strconv.ParseFloat(startingChannel[0], 64)
		if err == nil && startNum > 0 {
			firstFreeNumber = startNum
		}
	}

	for {
		if !slices.Contains(*allChannelNumbers, firstFreeNumber) {
			xChannelID = fmt.Sprintf("%g", firstFreeNumber)
			*allChannelNumbers = append(*allChannelNumbers, firstFreeNumber)
			return
		}
		firstFreeNumber++
	}
}

// generateChannelHash creates a hash for a channel based on its attributes.
func generateChannelHash(m3uID, name, groupTitle, tvgID, tvgName, uuidKey, uuidValue string) string {
	hash := md5.Sum([]byte(m3uID + name + groupTitle + tvgID + tvgName + uuidKey + uuidValue))
	return hex.EncodeToString(hash[:])
}

// processExistingXEPGChannel updates an existing channel in the XEPG database.
func processExistingXEPGChannel(m3uChannel M3UChannelStructXEPG, currentXEPGID string, channelHasUUID bool) (err error) {
	var xepgChannel = Data.XEPG.Channels[currentXEPGID]

	// Update Streaming URL
	xepgChannel.URL = m3uChannel.URL

	// Update Name, the Name is used to check whether the Channel is still available in a Playlist. Function: cleanupXEPG
	xepgChannel.Name = m3uChannel.Name

	// Update Channel Name, only possible with Channel ID's
	if channelHasUUID {
		if xepgChannel.XUpdateChannelName {
			xepgChannel.XName = m3uChannel.Name
		}
	}

	// Update GroupTitle
	xepgChannel.GroupTitle = m3uChannel.GroupTitle

	if xepgChannel.XUpdateChannelGroup {
		xepgChannel.XGroupTitle = m3uChannel.GroupTitle
	}

	// Update Channel Logo. Will be overwritten again if the Logo is present in the XMLTV file
	if xepgChannel.XUpdateChannelIcon {
		xepgChannel.TvgLogo = m3uChannel.TvgLogo
	}

	Data.XEPG.Channels[currentXEPGID] = xepgChannel
	return
}

// processNewXEPGChannel creates a new channel in the XEPG database.
func processNewXEPGChannel(m3uChannel M3UChannelStructXEPG, allChannelNumbers *[]float64) {
	var xepg = generateNewXEPGID()
	xChannelID := func() string {
		if m3uChannel.PreserveMapping == "true" {
			return findFreeChannelNumber(allChannelNumbers, m3uChannel.UUIDValue)
		} else {
			return findFreeChannelNumber(allChannelNumbers, m3uChannel.StartingChannel)
		}
	}()
	var newChannel XEPGChannelStruct
	newChannel.FileM3UID = m3uChannel.FileM3UID
	newChannel.FileM3UName = m3uChannel.FileM3UName
	newChannel.FileM3UPath = m3uChannel.FileM3UPath
	newChannel.Values = m3uChannel.Values
	newChannel.GroupTitle = m3uChannel.GroupTitle
	newChannel.Name = m3uChannel.Name
	newChannel.TvgID = m3uChannel.TvgID
	newChannel.TvgLogo = m3uChannel.TvgLogo
	newChannel.TvgShift = m3uChannel.TvgShift
	if m3uChannel.TvgShift == "" {
		newChannel.TvgShift = "0"
	} else {
		newChannel.TvgShift = m3uChannel.TvgShift
	}
	newChannel.URL = m3uChannel.URL
	newChannel.XmltvFile = ""
	newChannel.XMapping = ""

	if len(m3uChannel.UUIDKey) > 0 {
		newChannel.UUIDKey = m3uChannel.UUIDKey
		newChannel.UUIDValue = m3uChannel.UUIDValue
	}

	newChannel.XName = m3uChannel.Name
	newChannel.XGroupTitle = m3uChannel.GroupTitle
	newChannel.XEPG = xepg
	newChannel.XChannelID = xChannelID
	newChannel.XTimeshift = newChannel.TvgShift
	newChannel.XActive = true

	Data.XEPG.Channels[xepg] = newChannel
}

// Automatically assign Channels and check the Mapping
func mapping() (err error) {
	showInfo("XEPG:" + "Map channels")

	for xepgID, xepgChannel := range Data.XEPG.Channels {
		xepgChannel, _ = performAutomaticChannelMapping(xepgChannel, xepgID)

		if Settings.EnableMappedChannels && (xepgChannel.XmltvFile != "-" || xepgChannel.XMapping != "-") {
			xepgChannel.XActive = true
		}

		xepgChannel = verifyExistingChannelMappings(xepgChannel)

		Data.XEPG.Channels[xepgID] = xepgChannel // Update Data.XEPG.Channels with potentially modified xepgChannel
	}

	err = saveMapToJSONFile(System.File.XEPG, Data.XEPG.Channels)
	if err != nil {
		return
	}
	return
}

// performAutomaticChannelMapping attempts to automatically map an inactive channel.
// It returns the (potentially modified) channel and a boolean indicating if a mapping was made.
func performAutomaticChannelMapping(xepgChannel XEPGChannelStruct, _ string) (XEPGChannelStruct, bool) {
	mappingMade := false
	// Values can be "-", therefore len <= 1.
	// Only attempt automatic mapping if BOTH XmltvFile and XMapping are unassigned.
	if len(xepgChannel.XmltvFile) <= 1 && len(xepgChannel.XMapping) <= 1 {
		var tvgID = xepgChannel.TvgID
		// Set default for new Channel
		if Settings.DefaultMissingEPG != "-" {
			xepgChannel.XmltvFile = "xTeVe Dummy"
			xepgChannel.XMapping = Settings.DefaultMissingEPG
			mappingMade = true // Default mapping is a form of mapping
		} else {
			xepgChannel.XmltvFile = "-"
			xepgChannel.XMapping = "-"
			// mappingMade remains false if no default is set and no match is found later
		}
		// Data.XEPG.Channels[xepgID] = xepgChannel // This write should happen in the calling function or after all modifications

		mappingFound := false // Flag to indicate if a mapping has been found and we can exit the outer loop
		for file, xmltvChannels := range Data.XMLTV.Mapping {
			if mappingFound {
				break // Exit outer loop if mapping was found in a previous iteration
			}
			xmltvMap, ok := xmltvChannels.(map[string]any)
			if !ok {
				continue // Skip if type assertion fails
			}

			if channel, ok := xmltvMap[tvgID]; ok {
				channelData, ok := channel.(map[string]any)
				if !ok {
					// This case should ideally not happen if data structure is consistent
					continue
				}
				if channelID, ok := channelData["id"].(string); ok {
					xepgChannel.XmltvFile = file
					xepgChannel.XMapping = channelID
					mappingMade = true
					if icon, ok := channelData["icon"].(string); ok {
						if len(icon) > 0 {
							xepgChannel.TvgLogo = icon
						}
					}
					mappingFound = true // Set flag to break outer loop
					// No 'continue' here, loop will break due to mappingFound in the next iteration's check
				}
			} else if !mappingFound { // Only search by name if not already found by tvgID
				// Search for the proper XEPG channel ID by comparing its name with every alias in XML file
				for _, xmltvChannel := range xmltvMap {
					if mappingFound { // Check again in case inner loop found something in previous iteration
						break
					}
					channelData, ok := xmltvChannel.(map[string]any)
					if !ok {
						continue
					}
					displayNamesData, ok := channelData["display-names"]
					if !ok {
						continue
					}
					displayNamesArray, ok := displayNamesData.([]any)
					if !ok {
						concreteDisplayNames, okDisplayName := displayNamesData.([]DisplayName)
						if okDisplayName {
							displayNamesArray = make([]any, len(concreteDisplayNames))
							for i, dn := range concreteDisplayNames {
								displayNamesArray[i] = dn
							}
						} else {
							continue
						}
					}

					for _, nameEntry := range displayNamesArray {
						var currentDisplayNameValue string
						if dnStruct, ok := nameEntry.(DisplayName); ok {
							currentDisplayNameValue = dnStruct.Value
						} else if dnMap, ok := nameEntry.(map[string]any); ok {
							if val, ok := dnMap["Value"].(string); ok {
								currentDisplayNameValue = val
							} else {
								continue
							}
						} else {
							continue
						}

						xmltvNameSolid := strings.ReplaceAll(currentDisplayNameValue, " ", "")
						xepgNameSolid := strings.ReplaceAll(xepgChannel.Name, " ", "")

						if strings.EqualFold(xmltvNameSolid, xepgNameSolid) {
							if id, ok := channelData["id"].(string); ok {
								xepgChannel.XmltvFile = file
								xepgChannel.XMapping = id
								mappingMade = true
								if icon, ok := channelData["icon"].(string); ok {
									if len(icon) > 0 {
										xepgChannel.TvgLogo = icon
									}
								}
								mappingFound = true // Set flag to break outer and inner loops
								break               // Break from inner loop over displayNamesArray
							}
						}
					}
					// if mappingFound, the inner loop over xmltvMap will break in next iter
				}
			}
		}
	}
	return xepgChannel, mappingMade
}

// verifyExistingChannelMappings checks assigned XMLTV files and channels for active mappings.
// It returns the (potentially modified) channel.
func verifyExistingChannelMappings(xepgChannel XEPGChannelStruct) XEPGChannelStruct {
	if !xepgChannel.XActive {
		return xepgChannel
	}

	var mappingValue = xepgChannel.XMapping
	var file = xepgChannel.XmltvFile

	if file != "xTeVe Dummy" {
		xmltvFileMapping, fileExists := Data.XMLTV.Mapping[file].(map[string]any)
		if !fileExists {
			fileID := strings.TrimSuffix(getFilenameFromPath(file), path.Ext(getFilenameFromPath(file)))
			ShowError(fmt.Errorf("missing XMLTV file: %s", getProviderParameter(fileID, "xmltv", "name")), 0)
			showWarning(2301)
			xepgChannel.XActive = false
		} else {
			channelData, channelExists := xmltvFileMapping[mappingValue].(map[string]any)
			if !channelExists {
				ShowError(fmt.Errorf("missing EPG data: %s for mapping %s in file %s", xepgChannel.Name, mappingValue, file), 0)
				showWarning(2302)
				xepgChannel.XActive = false
			} else {
				// Update Channel Logo
				if logo, ok := channelData["icon"].(string); ok {
					if xepgChannel.XUpdateChannelIcon && len(logo) > 0 {
						xepgChannel.TvgLogo = logo
					}
				}
			}
		}
	}

	// If, after checks, the channel is no longer considered active (e.g., due to missing files/mappings),
	// or if XmltvFile/XMapping were empty to begin with for an active channel (which shouldn't happen if logic is correct prior),
	// ensure they are set to "-".
	if !xepgChannel.XActive || len(xepgChannel.XmltvFile) == 0 || len(xepgChannel.XMapping) == 0 {
		xepgChannel.XmltvFile = "-"
		xepgChannel.XMapping = "-"
	}
	return xepgChannel
}

// createChannelElements generates an XMLTV channel element.
func createChannelElements(xepgChannel XEPGChannelStruct, imgc *imgcache.Cache) *Channel {
	var channel Channel
	channel.ID = xepgChannel.XChannelID
	// Check if imgc is not nil and if the GetURL function is assigned within imgc.Image
	if imgc != nil && imgc.Image.GetURL != nil {
		channel.Icon = Icon{Src: imgc.Image.GetURL(xepgChannel.TvgLogo)}
	} else {
		// Fallback: use TvgLogo directly if no image cache or GetURL func is available.
		channel.Icon = Icon{Src: xepgChannel.TvgLogo}
	}
	channel.DisplayNames = append(channel.DisplayNames, DisplayName{Value: xepgChannel.XName})
	return &channel
}

// createProgramElements generates XMLTV program elements for a channel.
// It's a wrapper around getProgramData.
func createProgramElements(xepgChannel XEPGChannelStruct) ([]*Program, error) {
	tmpProgramXML, err := getProgramData(xepgChannel)
	if err != nil {
		return nil, err
	}
	return tmpProgramXML.Program, nil
}

// Create XMLTV File
func createXMLTVFile() (err error) {
	// Image Cache
	var imgc = Data.Cache.Images // This is *imgcache.Cache. imgc itself will be passed.

	Data.Cache.ImagesFiles = []string{}
	Data.Cache.ImagesURLS = []string{}
	Data.Cache.ImagesCache = []string{}

	files, err := os.ReadDir(System.Folder.ImagesCache)
	if err == nil {
		for _, file := range files {
			if !slices.Contains(Data.Cache.ImagesCache, file.Name()) {
				Data.Cache.ImagesCache = append(Data.Cache.ImagesCache, file.Name())
			}
		}
	}

	if len(Data.XMLTV.Files) == 0 && len(Data.Streams.Active) == 0 {
		Data.XEPG.Channels = make(map[string]XEPGChannelStruct)
		return
	}

	showInfo("XEPG:" + fmt.Sprintf("Create XMLTV file (%s)", System.File.XML))

	var xepgXML XMLTV
	xepgXML.Generator = System.Name

	xepgXML.Source = fmt.Sprintf("%s - %s.%s", System.Name, System.Version, System.Build)

	for _, xepgChannel := range Data.XEPG.Channels {
		if xepgChannel.XActive {
			// Create Channel Element
			channelElement := createChannelElements(xepgChannel, imgc) // Pass the whole imgc *imgcache.Cache
			xepgXML.Channel = append(xepgXML.Channel, channelElement)

			// Create Program Elements
			programElements, progErr := createProgramElements(xepgChannel) // Renamed err to progErr
			if progErr == nil {
				xepgXML.Program = append(xepgXML.Program, programElements...)
			} else {
				// Handle error from createProgramElements if necessary, e.g., log it
				ShowError(fmt.Errorf("error creating program elements for channel %s: %v", xepgChannel.XName, progErr), 0)
			}
		}
	}

	var content, _ = xml.MarshalIndent(xepgXML, "  ", "    ")
	var xmlOutput = []byte(xml.Header + string(content))
	err = writeByteToFile(System.File.XML, xmlOutput)
	if err != nil {
		return
	}

	showInfo("XEPG:" + fmt.Sprintf("Compress XMLTV file (%s)", System.Compressed.GZxml))
	err = compressGZIP(&xmlOutput, System.Compressed.GZxml) // Original err is shadowed here, this is fine.

	xepgXML = XMLTV{} // Clear struct for memory
	return            // Returns the error from compressGZIP or nil
}

// Create Program Data (createXMLTVFile)
func getProgramData(xepgChannel XEPGChannelStruct) (xepgXML XMLTV, err error) {
	var xmltvFile = System.Folder.Data + xepgChannel.XmltvFile
	var channelID = xepgChannel.XMapping
	var xmltv XMLTV

	if xmltvFile == System.Folder.Data+"xTeVe Dummy" {
		xmltv = createDummyProgram(xepgChannel)
	} else {
		err = getLocalXMLTV(xmltvFile, &xmltv)
		if err != nil {
			return
		}
	}

	for _, xmltvProgram := range xmltv.Program {
		if xmltvProgram.Channel == channelID {
			var program = &Program{}
			// Channel ID
			program.Channel = xepgChannel.XChannelID
			timeshift, _ := strconv.Atoi(xepgChannel.XTimeshift)
			progStart := strings.Split(xmltvProgram.Start, " ")
			progStop := strings.Split(xmltvProgram.Stop, " ")
			tzStart, _ := strconv.Atoi(progStart[1])
			tzStop, _ := strconv.Atoi(progStop[1])
			progStart[1] = fmt.Sprintf("%+05d", tzStart+timeshift*100)
			progStop[1] = fmt.Sprintf("%+05d", tzStop+timeshift*100)
			program.Start = strings.Join(progStart, " ")
			program.Stop = strings.Join(progStop, " ")

			// Title
			program.Title = xmltvProgram.Title

			// Subtitle
			program.SubTitle = xmltvProgram.SubTitle

			// Description
			program.Desc = xmltvProgram.Desc

			// Category
			getCategory(program, xmltvProgram, xepgChannel)

			// Credits
			program.Credits = xmltvProgram.Credits

			// Rating
			program.Rating = xmltvProgram.Rating

			// StarRating
			program.StarRating = xmltvProgram.StarRating

			// Country
			program.Country = xmltvProgram.Country

			// Program icon
			getPoster(program, xmltvProgram)

			// Language
			program.Language = xmltvProgram.Language

			// Episodes numbers
			getEpisodeNum(program, xmltvProgram, xepgChannel)

			// Video
			getVideo(program, xmltvProgram, xepgChannel)

			// Date
			program.Date = xmltvProgram.Date

			// Previously shown
			program.PreviouslyShown = xmltvProgram.PreviouslyShown

			// New
			program.New = xmltvProgram.New

			// Live
			program.Live = xmltvProgram.Live

			// Premiere
			program.Premiere = xmltvProgram.Premiere

			xepgXML.Program = append(xepgXML.Program, program)
		}
	}
	return
}

// Create Dummy Data (createXMLTVFile)
func createDummyProgram(xepgChannel XEPGChannelStruct) (dummyXMLTV XMLTV) {
	var imgc = Data.Cache.Images
	var currentTime = time.Now()
	var dateArray = strings.Fields(currentTime.String())
	var offset = " " + dateArray[2]
	var currentDay = currentTime.Format("20060102")
	var startTime, _ = time.Parse("20060102150405", currentDay+"000000")

	showInfo("Create Dummy Guide:" + "Time offset" + offset + " - " + xepgChannel.XName)

	var dl = strings.Split(xepgChannel.XMapping, "_")
	dummyLength, err := strconv.Atoi(dl[0])
	if err != nil {
		ShowError(err, 000)
		return
	}

	for d := range 4 {
		var epgStartTime = startTime.Add(time.Hour * time.Duration(d*24))

		for t := dummyLength; t <= 1440; t = t + dummyLength {
			var epgStopTime = epgStartTime.Add(time.Minute * time.Duration(dummyLength))
			var epg Program
			poster := Poster{}

			epg.Channel = xepgChannel.XMapping
			epg.Start = epgStartTime.Format("20060102150405") + offset
			epg.Stop = epgStopTime.Format("20060102150405") + offset
			epg.Title = append(epg.Title, &Title{Value: xepgChannel.XName + " (" + epgStartTime.Weekday().String()[0:2] + ". " + epgStartTime.Format("15:04") + " - " + epgStopTime.Format("15:04") + ")", Lang: "en"})

			if len(xepgChannel.XDescription) == 0 {
				epg.Desc = append(epg.Desc, &Desc{Value: "xTeVe: (" + strconv.Itoa(dummyLength) + " Minutes) " + epgStartTime.Weekday().String() + " " + epgStartTime.Format("15:04") + " - " + epgStopTime.Format("15:04"), Lang: "en"})
			} else {
				epg.Desc = append(epg.Desc, &Desc{Value: xepgChannel.XDescription, Lang: "en"})
			}

			if Settings.XepgReplaceMissingImages {
				poster.Src = imgc.Image.GetURL(xepgChannel.TvgLogo)
				epg.Poster = append(epg.Poster, poster)
			}

			if xepgChannel.XCategory != "Movie" {
				epg.EpisodeNum = append(epg.EpisodeNum, &EpisodeNum{Value: epgStartTime.Format("2006-01-02 15:04:05"), System: "original-air-date"})
			}
			epg.New = &New{Value: ""}
			dummyXMLTV.Program = append(dummyXMLTV.Program, &epg)
			epgStartTime = epgStopTime
		}
	}
	return
}

// Expand Categories (createXMLTVFile)
func getCategory(program *Program, xmltvProgram *Program, xepgChannel XEPGChannelStruct) {
	for _, i := range xmltvProgram.Category {
		category := &Category{}
		category.Value = i.Value
		category.Lang = i.Lang
		program.Category = append(program.Category, category)
	}

	if len(xepgChannel.XCategory) > 0 {
		category := &Category{}
		category.Value = xepgChannel.XCategory
		category.Lang = "en"
		program.Category = append(program.Category, category)
	}
}

// Load the Poster Cover Program from the XMLTV File
func getPoster(program *Program, xmltvProgram *Program) {
	var imgc = Data.Cache.Images

	for _, poster := range xmltvProgram.Poster {
		poster.Src = imgc.Image.GetURL(poster.Src)
		program.Poster = append(program.Poster, poster)
	}

	if Settings.XepgReplaceMissingImages {
		if len(xmltvProgram.Poster) == 0 {
			var poster Poster
			poster.Src = imgc.Image.GetURL(poster.Src)
			program.Poster = append(program.Poster, poster)
		}
	}
}

// Apply Episode system, if none is available and a Category has been set in the mapping, an Episode is created
func getEpisodeNum(program *Program, xmltvProgram *Program, xepgChannel XEPGChannelStruct) {
	program.EpisodeNum = xmltvProgram.EpisodeNum

	if len(xepgChannel.XCategory) > 0 && xepgChannel.XCategory != "Movie" {
		if len(xmltvProgram.EpisodeNum) == 0 {
			var timeLayout = "20060102150405"
			t, err := time.Parse(timeLayout, strings.Split(xmltvProgram.Start, " ")[0])
			if err == nil {
				program.EpisodeNum = append(program.EpisodeNum, &EpisodeNum{Value: t.Format("2006-01-02 15:04:05"), System: "original-air-date"})
			} else {
				ShowError(err, 0)
			}
		}
	}
}

// Create Video Parameters (createXMLTVFile)
func getVideo(program *Program, xmltvProgram *Program, xepgChannel XEPGChannelStruct) {
	var video Video
	video.Present = xmltvProgram.Video.Present
	video.Colour = xmltvProgram.Video.Colour
	video.Aspect = xmltvProgram.Video.Aspect
	video.Quality = xmltvProgram.Video.Quality

	if len(xmltvProgram.Video.Quality) == 0 {
		if strings.Contains(strings.ToUpper(xepgChannel.XName), " HD") || strings.Contains(strings.ToUpper(xepgChannel.XName), " FHD") {
			video.Quality = "HDTV"
		}
		if strings.Contains(strings.ToUpper(xepgChannel.XName), " UHD") || strings.Contains(strings.ToUpper(xepgChannel.XName), " 4K") {
			video.Quality = "UHDTV"
		}
	}
	program.Video = video
}

// Load Local Provider XMLTV file
func getLocalXMLTV(file string, xmltv *XMLTV) (err error) {
	if _, ok := Data.Cache.XMLTV[file]; !ok {
		// Initialize Cache
		if len(Data.Cache.XMLTV) == 0 {
			Data.Cache.XMLTV = make(map[string]XMLTV)
		}
		// Read XML Data
		content, err := readByteFromFile(file)
		// Local XML File does not exist in the folder: Data
		if err != nil {
			ShowError(err, 1004)
			err = errors.New("local copy of the file no longer exists")
			return err
		}

		// Parse XML File
		err = xml.Unmarshal(content, &xmltv)
		if err != nil {
			return err
		}
		Data.Cache.XMLTV[file] = *xmltv
	} else {
		*xmltv = Data.Cache.XMLTV[file]
	}
	return
}

// Create M3U File
func createM3UFile() error { // Added error return type
	showInfo("XEPG:" + fmt.Sprintf("Create M3U file (%s)", System.File.M3U))
	_, err := buildM3U([]string{})
	if err != nil {
		ShowError(err, 000)
		return err // Propagate error
	}
	err = saveMapToJSONFile(System.File.URLS, Data.Cache.StreamingURLS)
	if err != nil {
		ShowError(err, 000) // Show error, but also return it
		return err
	}
	return nil
}

// Clean up the XEPG Database
func cleanupXEPG() {
	var sourceIDs []string

	for source := range Settings.Files.M3U {
		sourceIDs = append(sourceIDs, source)
	}

	for source := range Settings.Files.HDHR {
		sourceIDs = append(sourceIDs, source)
	}

	showInfo("XEPG:" + "Cleanup database")
	Data.XEPG.XEPGCount = 0

	for id, xepgChannel := range Data.XEPG.Channels {
		if !slices.Contains(Data.Cache.Streams.Active, xepgChannel.Name+xepgChannel.FileM3UID) {
			delete(Data.XEPG.Channels, id)
			continue
		}
		if !slices.Contains(sourceIDs, xepgChannel.FileM3UID) {
			delete(Data.XEPG.Channels, id)
			continue
		}
		if xepgChannel.XActive {
			Data.XEPG.XEPGCount++
		}
	}

	err := saveMapToJSONFile(System.File.XEPG, Data.XEPG.Channels)
	if err != nil {
		ShowError(err, 000)
		return
	}

	showInfo("XEPG Channels:" + fmt.Sprintf("%d", Data.XEPG.XEPGCount))

	if len(Data.Streams.Active) > 0 && Data.XEPG.XEPGCount == 0 {
		showWarning(2005)
	}
}

// clearXMLTVCache empties XMLTV cache and runs a garbage collector
func clearXMLTVCache() {
	Data.Cache.XMLTV = make(map[string]XMLTV)
	runtime.GC()
}

// bindMapToM3UChannelStruct manually assigns fields from map[string]string to M3UChannelStructXEPG.
// This is significantly faster than using bindToStruct (JSON marshal/unmarshal).
func bindMapToM3UChannelStruct(data map[string]string, target *M3UChannelStructXEPG) {
	if val, ok := data["_file.m3u.id"]; ok {
		target.FileM3UID = val
	}
	if val, ok := data["_file.m3u.name"]; ok {
		target.FileM3UName = val
	}
	if val, ok := data["_file.m3u.path"]; ok {
		target.FileM3UPath = val
	}
	if val, ok := data["group-title"]; ok {
		target.GroupTitle = val
	}
	if val, ok := data["name"]; ok {
		target.Name = val
	}
	if val, ok := data["tvg-id"]; ok {
		target.TvgID = val
	}
	if val, ok := data["tvg-logo"]; ok {
		target.TvgLogo = val
	}
	if val, ok := data["tvg-name"]; ok {
		target.TvgName = val
	}
	if val, ok := data["tvg-shift"]; ok {
		target.TvgShift = val
	}
	if val, ok := data["url"]; ok {
		target.URL = val
	}
	if val, ok := data["_uuid.key"]; ok {
		target.UUIDKey = val
	}
	if val, ok := data["_uuid.value"]; ok {
		target.UUIDValue = val
	}
	if val, ok := data["_values"]; ok {
		target.Values = val
	}
	if val, ok := data["_preserve-mapping"]; ok {
		target.PreserveMapping = val
	}
	if val, ok := data["_starting-channel"]; ok {
		target.StartingChannel = val
	}
}
