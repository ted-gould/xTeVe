package src

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	m3u "xteve/src/internal/m3u-parser"
)

// fileType: Which File Type should be updated (m3u, hdhr, xml) | fileID: Update a specific File (Provider ID)
func getProviderData(ctx context.Context, fileType, fileID string) (err error) {
	var fileExtension, serverFileName string
	var body = make([]byte, 0)
	// var newProvider = false // Removed: Ineffectual assignment
	var dataMap = make(map[string]any)

	var saveDateFromProvider = func(fileSource, serverFileName, id string, body []byte) (err error) {
		var data = make(map[string]any)

		if value, ok := dataMap[id].(map[string]any); ok {
			data = value
		} else {
			data["id.provider"] = id
			dataMap[id] = data
		}

		// Default keys for the Provider Data
		var keys = []string{"name", "description", "type", "file." + System.AppName, "file.source", "tuner", "last.update", "compatibility", "counter.error", "counter.download", "provider.availability"}

		for _, key := range keys {
			if _, ok := data[key]; !ok {
				switch key {
				case "name":
					data[key] = serverFileName
				case "description":
					data[key] = ""
				case "type":
					data[key] = fileType
				case "file." + System.AppName:
					data[key] = id + fileExtension
				case "file.source":
					data[key] = fileSource
				case "last.update":
					data[key] = time.Now().Format("2006-01-02 15:04:05")
				case "tuner":
					if fileType == "m3u" || fileType == "hdhr" {
						if _, ok := data[key].(float64); !ok {
							data[key] = 1
						}
					}
				case "compatibility":
					data[key] = make(map[string]any)
				case "counter.download":
					data[key] = 0.0
				case "counter.error":
					data[key] = 0.0
				case "provider.availability":
					data[key] = 100
				}
			}
		}

		if _, ok := data["id.provider"]; !ok {
			data["id.provider"] = id
		}

		// Extract File
		body, err = extractGZIP(body, fileSource)
		if err != nil {
			ShowError(err, 000)
			return
		}

		// Check Data
		showInfo("Check File:" + fileSource)
		switch fileType {
		case "m3u":
			_, err = m3u.MakeInterfaceFromM3U(body)
		case "hdhr":
			_, err = jsonToInterface(string(body))
		case "xmltv":
			err = checkXMLCompatibility(id, body)
		}

		if err != nil {
			return
		}

		var filePath string
		if f, ok := data["file."+System.AppName].(string); ok {
			filePath = System.Folder.Data + f
		} else {
			return fmt.Errorf("invalid file path in provider data")
		}

		err = writeByteToFile(filePath, body)

		if err == nil {
			data["last.update"] = time.Now().Format("2006-01-02 15:04:05")
			if v, ok := data["counter.download"].(float64); ok {
				data["counter.download"] = v + 1
			}
		}
		return
	}

	switch fileType {
	case "m3u":
		dataMap = Settings.Files.M3U
		fileExtension = ".m3u"
	case "hdhr":
		dataMap = Settings.Files.HDHR
		fileExtension = ".json"
	case "xmltv":
		dataMap = Settings.Files.XMLTV
		fileExtension = ".xml"
	}

	for dataID, d := range dataMap {
		var data, ok = d.(map[string]any)
		if !ok {
			continue
		}
		var fileSource, okSource = data["file.source"].(string)
		if !okSource {
			continue
		}
		var newProvider = false // Declare and initialize newProvider inside the loop

		if _, ok := data["new"]; ok {
			newProvider = true
			delete(data, "new")
		}

		// If an ID is available and does not match the one from the Database, the Update is skipped (goto)
		if len(fileID) > 0 && !newProvider {
			if dataID != fileID {
				goto Done
			}
		}

		switch fileType {
		case "hdhr":
			// Load from the HDHomeRun Tuner
			showInfo("Tuner:" + fileSource)
			var tunerURL = "http://" + fileSource + "/lineup.json"
			serverFileName, body, err = downloadFileFromServer(ctx, tunerURL)
		default:
			if strings.Contains(fileSource, "http://") || strings.Contains(fileSource, "https://") {
				// Load from the Remote Server
				showInfo("Download:" + fileSource)
				serverFileName, body, err = downloadFileFromServer(ctx, fileSource)
			} else {
				// Load a local File
				showInfo("Open:" + fileSource)

				err = checkFile(fileSource)
				if err == nil {
					body, err = readByteFromFile(fileSource)
					serverFileName = getFilenameFromPath(fileSource)
				}
			}
		}

		if err == nil {
			err = saveDateFromProvider(fileSource, serverFileName, dataID, body)
			if err == nil {
				showInfo("Save File:" + fileSource + " [ID: " + dataID + "]")
			}
		}

		if err != nil {
			ShowError(err, 000)
			var downloadErr = err

			if !newProvider {
				// Check whether there is an older File
				var file = System.Folder.Data + dataID + fileExtension

				err = checkFile(file)
				if err == nil {
					if len(fileID) == 0 {
						showWarning(1011)
					}
					err = downloadErr
				}

				// Increase Error Counter by 1
				if value, ok := dataMap[dataID].(map[string]any); ok {
					// Directly modify the map obtained from dataMap
					if v, ok := value["counter.error"].(float64); ok {
						value["counter.error"] = v + 1
					}
					if v, ok := value["counter.download"].(float64); ok {
						value["counter.download"] = v + 1
					}
					// No need for the separate 'data' variable here
				}
			} else {
				return downloadErr
			}
		}

		// Calculate the Margin of Error
		if !newProvider {
			if value, ok := dataMap[dataID].(map[string]any); ok {
				var counterError, okError = value["counter.error"].(float64)
				var counterDownload, okDownload = value["counter.download"].(float64)
				if okError && okDownload {
					if counterError == 0 {
						value["provider.availability"] = 100
					} else {
						value["provider.availability"] = int(counterError*100/counterDownload*-1 + 100)
					}
				}
			}
		}

		switch fileType {
		case "m3u":
			Settings.Files.M3U = dataMap
		case "hdhr":
			Settings.Files.HDHR = dataMap
		case "xmltv":
			Settings.Files.XMLTV = dataMap
			delete(Data.Cache.XMLTV, System.Folder.Data+dataID+fileExtension)
		}
		if err := saveSettings(Settings); err != nil {
			ShowError(err, 0)
		}
	Done:
	}
	return
}

func downloadFileFromServer(ctx context.Context, providerURL string) (filename string, body []byte, err error) {
	_, err = url.ParseRequestURI(providerURL)
	if err != nil {
		return
	}

	req, err := http.NewRequestWithContext(ctx, "GET", providerURL, nil)
	if err != nil {
		return
	}

	req.Header.Set("User-Agent", Settings.UserAgent)

	client := NewHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err = fmt.Errorf("%d: %s "+http.StatusText(resp.StatusCode), resp.StatusCode, providerURL)
		return
	}

	// Get the File Mame from the Header
	var index = strings.Index(resp.Header.Get("Content-Disposition"), "filename")

	if index > -1 {
		var headerFilename = resp.Header.Get("Content-Disposition")[index:len(resp.Header.Get("Content-Disposition"))]
		var value = strings.Split(headerFilename, `=`)
		var f = strings.Replace(value[1], `"`, "", -1)

		f = strings.Replace(f, `;`, "", -1)
		filename = f
		showInfo("Header filename:" + filename)
	} else {
		var cleanFilename = strings.SplitN(getFilenameFromPath(providerURL), "?", 2)
		filename = cleanFilename[0]
	}

	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return
	}
	return
}
