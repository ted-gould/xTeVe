package src

import (
	"bufio"
	"encoding/xml"
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"

	"crypto/md5"
	"encoding/hex"
	"strconv"
	"strings"
	"sync"
	"time"

	"xteve/src/internal/imgcache"
)

var (
	// xmltvProgramIndices caches the mapping from ChannelID to Programs for each XMLTV file.
	// Map: XMLTV Filename -> ChannelID -> Slice of Program pointers
	xmltvProgramIndices = make(map[string]map[string][]*Program)
	xmltvProgramMutex   sync.RWMutex
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
	Data.XMLTV.Mapping = make(map[string]map[string]XMLTVChannelMapping)

	var tmpMap = make(map[string]map[string]XMLTVChannelMapping)

	if len(Data.XMLTV.Files) > 0 {
		for i := len(Data.XMLTV.Files) - 1; i >= 0; i-- {
			var file = Data.XMLTV.Files[i]
			var fileID = strings.TrimSuffix(filepath.Base(file), path.Ext(filepath.Base(file)))
			showInfo("XEPG:" + "Parse XMLTV file: " + getProviderParameter(fileID, "xmltv", "name"))

			var xmltv XMLTV

			err := getLocalXMLTV(file, &xmltv)
			if err != nil {
				Data.XMLTV.Files = slices.Delete(Data.XMLTV.Files, i, i+1)
				var errMsg = err.Error()
				err = errors.New(getProviderParameter(fileID, "xmltv", "name") + ": " + errMsg)
				ShowError(err, 000)
			}

			// XML Parsing (Provider File)
			if err == nil {
				// Write Data from the XML File to a temporary Map
				var xmltvMap = make(map[string]XMLTVChannelMapping)

				for _, c := range xmltv.Channel {
					var channel XMLTVChannelMapping
					channel.ID = c.ID
					channel.DisplayNames = c.DisplayNames
					channel.Icon = c.Icon.Src
					xmltvMap[c.ID] = channel
				}
				tmpMap[filepath.Base(file)] = xmltvMap
				Data.XMLTV.Mapping[filepath.Base(file)] = xmltvMap
			}
		}
		Data.XMLTV.Mapping = tmpMap
	} else {
		if !System.ConfigurationWizard {
			showWarning(1007)
		}
	}

	// Create selection for the Dummy
	var dummy = make(map[string]XMLTVChannelMapping)
	var times = []string{"30", "60", "90", "120", "180", "240", "360"}

	for _, i := range times {
		var dummyChannel XMLTVChannelMapping
		dummyChannel.DisplayNames = []DisplayName{{Value: i + " Minutes"}}
		dummyChannel.ID = i + "_Minutes"
		dummyChannel.Icon = ""
		dummy[dummyChannel.ID] = dummyChannel
	}
	Data.XMLTV.Mapping["xTeVe Dummy"] = dummy
}

// Create / update XEPG Database
func createXEPGDatabase() (err error) {
	// Optimization: Pre-allocate slice capacity to avoid reallocations
	Data.Cache.Streams.Active = make([]string, 0, len(Data.Streams.Active))

	Data.XEPG.Channels, err = loadXEPGChannels(System.File.XEPG)
	if err != nil {
		ShowError(err, 1004)
		return err
	}

	showInfo("XEPG:" + "Update database")

	// Optimization: Pre-allocate slice capacity based on loaded channels
	var allChannelNumbers = make(map[float64]bool, len(Data.XEPG.Channels))

	// Delete Channel with missing Channel Numbers.
	for id, xepgChannel := range Data.XEPG.Channels {
		if len(xepgChannel.XChannelID) == 0 {
			delete(Data.XEPG.Channels, id)
		}

		if xChannelID, err := strconv.ParseFloat(xepgChannel.XChannelID, 64); err == nil {
			allChannelNumbers[xChannelID] = true
		}
	}

	// Make a map of the db channels based on their previously downloaded attributes -- filename, group, title, etc
	var xepgChannelsValuesMap = make(map[string]XEPGChannelStruct, len(Data.XEPG.Channels))

	// Optimization: Indices to speed up the slow path lookup
	// Map: FileM3UID -> Name -> *Channel
	channelsByName := make(map[string]map[string]*XEPGChannelStruct)
	// Map: FileM3UID -> UUIDKey -> UUIDValue -> *Channel
	channelsByUUID := make(map[string]map[string]map[string]*XEPGChannelStruct)
	// Map: FileM3UID -> List of channels that have Regex rules
	channelsWithRegex := make(map[string][]*XEPGChannelStruct)

	for _, channel := range Data.XEPG.Channels {
		// Create a copy of the channel to safely take its address for the indices
		c := channel

		if len(c.UpdateChannelNameRegex) > 0 {
			c.CompiledNameRegex, err = regexp.Compile(c.UpdateChannelNameRegex)
			if err != nil {
				ShowError(err, 1018)
			}
		}

		if len(c.UpdateChannelNameByGroupRegex) > 0 {
			c.CompiledGroupRegex, err = regexp.Compile(c.UpdateChannelNameByGroupRegex)
			if err != nil {
				ShowError(err, 1018)
			}
		}
		channelHash := generateChannelHash(c.FileM3UID, c.Name, c.GroupTitle, c.TvgID, c.TvgName, c.UUIDKey, c.UUIDValue)
		xepgChannelsValuesMap[channelHash] = c

		// Populate indices
		ptr := &c

		if _, ok := channelsByName[c.FileM3UID]; !ok {
			channelsByName[c.FileM3UID] = make(map[string]*XEPGChannelStruct)
		}
		channelsByName[c.FileM3UID][c.Name] = ptr

		if len(c.UUIDValue) > 0 && len(c.UUIDKey) > 0 {
			if _, ok := channelsByUUID[c.FileM3UID]; !ok {
				channelsByUUID[c.FileM3UID] = make(map[string]map[string]*XEPGChannelStruct)
			}
			if _, ok := channelsByUUID[c.FileM3UID][c.UUIDKey]; !ok {
				channelsByUUID[c.FileM3UID][c.UUIDKey] = make(map[string]*XEPGChannelStruct)
			}
			channelsByUUID[c.FileM3UID][c.UUIDKey][c.UUIDValue] = ptr
		}

		if len(c.UpdateChannelNameRegex) > 0 {
			channelsWithRegex[c.FileM3UID] = append(channelsWithRegex[c.FileM3UID], ptr)
		}
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
			// SLOW PATH OPTIMIZED: Use indices instead of linear scan
			var match *XEPGChannelStruct

			// 1. Check UUID match
			if len(m3uChannel.UUIDValue) > 0 && len(m3uChannel.UUIDKey) > 0 {
				if uMap, ok := channelsByUUID[m3uChannel.FileM3UID]; ok {
					if vMap, ok := uMap[m3uChannel.UUIDKey]; ok {
						if found, ok := vMap[m3uChannel.UUIDValue]; ok {
							match = found
						}
					}
				}
			}

			// 2. Check Name match (if no UUID match found yet)
			if match == nil {
				if nMap, ok := channelsByName[m3uChannel.FileM3UID]; ok {
					if found, ok := nMap[m3uChannel.Name]; ok {
						match = found
					}
				}
			}

			// 3. Check Regex (only if no match yet)
			if match == nil {
				if candidates, ok := channelsWithRegex[m3uChannel.FileM3UID]; ok {
					for _, dxc := range candidates {
						// Guard against the situation when names are the same (already checked above implicitly by 'match == nil' but good for sanity)
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

						// Regex Match Found
						match = dxc

						showInfo("XEPG:" + fmt.Sprintf("Renaming the channel '%v' to '%v'", dxc.Name, m3uChannel.Name))
						channelExists = true
						match.XName = m3uChannel.Name
						currentXEPGID = match.XEPG
						// Save the modified channel back to the global state immediately
						Data.XEPG.Channels[currentXEPGID] = *match
						break
					}
				}
			}

			if match != nil {
				// If match was found (via UUID or Name) but NOT via regex (channelExists is still false)
				if !channelExists {
					channelExists = true
					currentXEPGID = match.XEPG

					if len(match.UUIDValue) > 0 && len(m3uChannel.UUIDValue) > 0 {
						if match.UUIDValue == m3uChannel.UUIDValue && match.UUIDKey == m3uChannel.UUIDKey {
							channelHasUUID = true
						}
					}
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
			processNewXEPGChannel(m3uChannel, allChannelNumbers)
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
func findFreeChannelNumber(allChannelNumbers map[float64]bool, startingChannel ...string) (xChannelID string) {
	// slices.Sort(*allChannelNumbers) -> Removed as we use a map now

	var firstFreeNumber float64 = Settings.MappingFirstChannel
	if len(startingChannel) > 0 && startingChannel[0] != "" {
		var startNum, err = strconv.ParseFloat(startingChannel[0], 64)
		if err == nil && startNum > 0 {
			firstFreeNumber = startNum
		}
	}

	for {
		// O(1) lookup
		if !allChannelNumbers[firstFreeNumber] {
			xChannelID = fmt.Sprintf("%g", firstFreeNumber)
			allChannelNumbers[firstFreeNumber] = true
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
func processNewXEPGChannel(m3uChannel M3UChannelStructXEPG, allChannelNumbers map[float64]bool) {
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

	// Optimization: Build Index for O(1) channel name lookup
	// Map: XMLTV File ID -> Normalized Name -> Channel ID
	var xmltvNameIndex = make(map[string]map[string]string)

	for file, xmltvChannels := range Data.XMLTV.Mapping {
		xmltvNameIndex[file] = make(map[string]string)
		for _, channel := range xmltvChannels {
			for _, displayName := range channel.DisplayNames {
				normalizedName := strings.ToLower(strings.ReplaceAll(displayName.Value, " ", ""))
				// Only store first occurrence if duplicates exist, mimicking finding first match in loop
				if _, exists := xmltvNameIndex[file][normalizedName]; !exists {
					xmltvNameIndex[file][normalizedName] = channel.ID
				}
			}
		}
	}

	for xepgID, xepgChannel := range Data.XEPG.Channels {
		xepgChannel, _ = performAutomaticChannelMapping(xepgChannel, xepgID, xmltvNameIndex)

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
func performAutomaticChannelMapping(xepgChannel XEPGChannelStruct, _ string, xmltvNameIndex map[string]map[string]string) (XEPGChannelStruct, bool) {
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

			if channel, ok := xmltvChannels[tvgID]; ok {
				xepgChannel.XmltvFile = file
				xepgChannel.XMapping = channel.ID
				mappingMade = true
				if len(channel.Icon) > 0 {
					xepgChannel.TvgLogo = channel.Icon
				}
				mappingFound = true // Set flag to break outer loop
				// No 'continue' here, loop will break due to mappingFound in the next iteration's check
			} else if !mappingFound { // Only search by name if not already found by tvgID
				// Optimization: Pre-calculate the solid name for the XEPG channel once
				xepgNameSolid := strings.ToLower(strings.ReplaceAll(xepgChannel.Name, " ", ""))

				// Optimized O(1) lookup using pre-computed index
				if nameMap, ok := xmltvNameIndex[file]; ok {
					if channelID, ok := nameMap[xepgNameSolid]; ok {
						if xmltvChannel, ok := xmltvChannels[channelID]; ok {
							xepgChannel.XmltvFile = file
							xepgChannel.XMapping = xmltvChannel.ID
							mappingMade = true
							if len(xmltvChannel.Icon) > 0 {
								xepgChannel.TvgLogo = xmltvChannel.Icon
							}
							mappingFound = true
						}
					}
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
		xmltvFileMapping, fileExists := Data.XMLTV.Mapping[file]
		if !fileExists {
			fileID := strings.TrimSuffix(filepath.Base(file), path.Ext(filepath.Base(file)))
			ShowError(fmt.Errorf("missing XMLTV file: %s", getProviderParameter(fileID, "xmltv", "name")), 0)
			showWarning(2301)
			xepgChannel.XActive = false
		} else {
			channelData, channelExists := xmltvFileMapping[mappingValue]
			if !channelExists {
				ShowError(fmt.Errorf("missing EPG data: %s for mapping %s in file %s", xepgChannel.Name, mappingValue, file), 0)
				showWarning(2302)
				xepgChannel.XActive = false
			} else {
				// Update Channel Logo
				if xepgChannel.XUpdateChannelIcon && len(channelData.Icon) > 0 {
					xepgChannel.TvgLogo = channelData.Icon
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
func createProgramElements(xepgChannel XEPGChannelStruct, programs *[]*Program) error {
	return getProgramData(xepgChannel, programs)
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
		// Optimization: Pre-allocate slice capacity and remove redundant linear scan (O(N^2) -> O(N))
		Data.Cache.ImagesCache = make([]string, 0, len(files))
		for _, file := range files {
			Data.Cache.ImagesCache = append(Data.Cache.ImagesCache, file.Name())
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
			progErr := createProgramElements(xepgChannel, &xepgXML.Program) // Renamed err to progErr
			if progErr != nil {
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
func getProgramData(xepgChannel XEPGChannelStruct, acc *[]*Program) (err error) {
	var xmltvFile = System.Folder.Data + xepgChannel.XmltvFile
	var channelID = xepgChannel.XMapping
	var xmltv XMLTV

	var programs []*Program

	if xmltvFile == System.Folder.Data+"xTeVe Dummy" {
		xmltv = createDummyProgram(xepgChannel)
		programs = xmltv.Program
	} else {
		err = getLocalXMLTV(xmltvFile, &xmltv)
		if err != nil {
			return
		}

		// Use index to find programs efficiently
		xmltvProgramMutex.RLock()
		fileIndex, exists := xmltvProgramIndices[xmltvFile]
		xmltvProgramMutex.RUnlock()

		if !exists {
			// Build index for this file
			xmltvProgramMutex.Lock()
			// Double check locking
			if _, ok := xmltvProgramIndices[xmltvFile]; !ok {
				newIndex := make(map[string][]*Program)
				for _, p := range xmltv.Program {
					newIndex[p.Channel] = append(newIndex[p.Channel], p)
				}
				xmltvProgramIndices[xmltvFile] = newIndex
			}
			fileIndex = xmltvProgramIndices[xmltvFile]
			xmltvProgramMutex.Unlock()
		}

		if pList, ok := fileIndex[channelID]; ok {
			programs = pList
		}
	}

	// Pre-calculate uppercase channel name to avoid repeated calls in getVideo
	// and extract other fields to avoid passing the whole struct
	upperChannelName := strings.ToUpper(xepgChannel.XName)
	xCategory := xepgChannel.XCategory

	// Optimization: Pre-allocate slice capacity to avoid reallocations
	if len(programs) > 0 {
		*acc = slices.Grow(*acc, len(programs))
	}

	// Optimization: Parse timeshift once outside the loop
	timeshift, _ := strconv.Atoi(xepgChannel.XTimeshift)

	for _, xmltvProgram := range programs {
		// No need to check channelID match again, index guarantees it
		var program = &Program{}
		// Channel ID
		program.Channel = xepgChannel.XChannelID

		program.Start = adjustProgramTime(xmltvProgram.Start, timeshift)
		program.Stop = adjustProgramTime(xmltvProgram.Stop, timeshift)

		// Title
		program.Title = xmltvProgram.Title

		// Subtitle
		program.SubTitle = xmltvProgram.SubTitle

		// Description
		program.Desc = xmltvProgram.Desc

		// Category
		getCategory(program, xmltvProgram, xCategory)

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
		getEpisodeNum(program, xmltvProgram, xCategory)

		// Video
		getVideo(program, xmltvProgram, upperChannelName)

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

		*acc = append(*acc, program)
	}
	return
}

// adjustProgramTime adjusts the timezone of a program start/stop time string.
// t format is expected to be "YYYYMMDDhhmmss +ZZZZ".
func adjustProgramTime(t string, timeshift int) string {
	if timeshift == 0 {
		return t
	}

	before, after, found := strings.Cut(t, " ")
	if !found {
		return t
	}

	tzStart, _ := strconv.Atoi(after)
	newTz := tzStart + timeshift*100

	// Optimization: Use strings.Builder and manual formatting to avoid fmt.Sprintf allocations.
	// This reduces allocations from 2 to 1 (result string) per call.
	var b strings.Builder
	b.Grow(len(before) + 6)
	b.WriteString(before)
	b.WriteByte(' ')

	if newTz < 0 {
		b.WriteByte('-')
		newTz = -newTz
	} else {
		b.WriteByte('+')
	}

	if newTz < 10 {
		b.WriteString("000")
	} else if newTz < 100 {
		b.WriteString("00")
	} else if newTz < 1000 {
		b.WriteString("0")
	}

	var buf [10]byte
	b.Write(strconv.AppendInt(buf[:0], int64(newTz), 10))

	return b.String()
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
func getCategory(program *Program, xmltvProgram *Program, xCategory string) {
	// Optimization: If no extra category is needed, reuse the source slice.
	// This avoids allocating a new slice header and backing array.
	if len(xCategory) == 0 {
		program.Category = xmltvProgram.Category
		return
	}

	// Optimization: Pre-allocate slice capacity
	targetLen := len(xmltvProgram.Category) + 1

	program.Category = make([]*Category, 0, targetLen)

	// Direct append to avoid allocations.
	// xmltvProgram.Category elements are immutable so we can safely share pointers.
	program.Category = append(program.Category, xmltvProgram.Category...)

	category := &Category{}
	category.Value = xCategory
	category.Lang = "en"
	program.Category = append(program.Category, category)
}

// Load the Poster Cover Program from the XMLTV File
func getPoster(program *Program, xmltvProgram *Program) {
	var imgc = Data.Cache.Images

	// Optimization: Pre-allocate slice capacity
	targetLen := len(xmltvProgram.Poster)
	if Settings.XepgReplaceMissingImages && targetLen == 0 {
		targetLen = 1
	}
	if targetLen > 0 {
		program.Poster = make([]Poster, 0, targetLen)
	}

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
func getEpisodeNum(program *Program, xmltvProgram *Program, xCategory string) {
	program.EpisodeNum = xmltvProgram.EpisodeNum

	if len(xCategory) > 0 && xCategory != "Movie" {
		if len(xmltvProgram.EpisodeNum) == 0 {
			// Optimization: Avoid time.Parse and allocations for standard format
			var formattedTime string
			var err error

			// Extract date part (YYYYMMDDhhmmss)
			datePart := xmltvProgram.Start
			if idx := strings.IndexByte(datePart, ' '); idx != -1 {
				datePart = datePart[:idx]
			}

			if len(datePart) == 14 {
				// Fast path: Construct string directly
				// YYYYMMDDhhmmss -> YYYY-MM-DD hh:mm:ss
				var sb strings.Builder
				sb.Grow(19)
				sb.WriteString(datePart[0:4])
				sb.WriteByte('-')
				sb.WriteString(datePart[4:6])
				sb.WriteByte('-')
				sb.WriteString(datePart[6:8])
				sb.WriteByte(' ')
				sb.WriteString(datePart[8:10])
				sb.WriteByte(':')
				sb.WriteString(datePart[10:12])
				sb.WriteByte(':')
				sb.WriteString(datePart[12:14])
				formattedTime = sb.String()
			} else {
				// Fallback to slow path
				var timeLayout = "20060102150405"
				var t time.Time
				t, err = time.Parse(timeLayout, datePart)
				if err == nil {
					formattedTime = t.Format("2006-01-02 15:04:05")
				}
			}

			if err == nil && len(formattedTime) > 0 {
				program.EpisodeNum = append(program.EpisodeNum, &EpisodeNum{Value: formattedTime, System: "original-air-date"})
			} else if err != nil {
				ShowError(err, 0)
			}
		}
	}
}

// Create Video Parameters (createXMLTVFile)
func getVideo(program *Program, xmltvProgram *Program, channelNameUpper string) {
	var video Video
	video.Present = xmltvProgram.Video.Present
	video.Colour = xmltvProgram.Video.Colour
	video.Aspect = xmltvProgram.Video.Aspect
	video.Quality = xmltvProgram.Video.Quality

	if len(xmltvProgram.Video.Quality) == 0 {
		if strings.Contains(channelNameUpper, " HD") || strings.Contains(channelNameUpper, " FHD") {
			video.Quality = "HDTV"
		}
		if strings.Contains(channelNameUpper, " UHD") || strings.Contains(channelNameUpper, " 4K") {
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
		f, err := os.Open(getPlatformFile(file))
		if err != nil {
			ShowError(err, 1004)
			return errors.New("local copy of the file no longer exists")
		}
		defer f.Close()

		// Parse XML File
		// Optimization: Stream decode to avoid loading entire file into memory
		err = xml.NewDecoder(f).Decode(xmltv)
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

	var filename = getPlatformFile(System.File.M3U)
	f, err := os.Create(filename)
	if err != nil {
		ShowError(err, 000)
		return err
	}
	defer f.Close()

	bw := bufio.NewWriter(f)
	err = buildM3UToWriter(bw, []string{})
	if err != nil {
		ShowError(err, 000)
		return err
	}
	if err = bw.Flush(); err != nil {
		ShowError(err, 000)
		return err
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
	sourceIDs := slices.Collect(maps.Keys(Settings.Files.M3U))
	sourceIDs = slices.AppendSeq(sourceIDs, maps.Keys(Settings.Files.HDHR))

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

	xmltvProgramMutex.Lock()
	xmltvProgramIndices = make(map[string]map[string][]*Program)
	xmltvProgramMutex.Unlock()

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
