package src

import (
	"errors"
	"fmt"
	"reflect"
)

func conditionalUpdateChanges() (err error) {
checkVersion:
	settingsMap, err := loadJSONFileToMap(System.File.Settings)
	if err != nil || len(settingsMap) == 0 {
		return
	}

	if settingsVersion, ok := settingsMap["version"].(string); ok {
		if settingsVersion > System.DBVersion {
			showInfo("Settings DB Version:" + settingsVersion)
			showInfo("System DB Version:" + System.DBVersion)
			err = errors.New(getErrMsg(1031))
			return
		}

		// Latest Compatible Version (1.4.4)
		if settingsVersion < System.Compatibility {
			err = errors.New(getErrMsg(1013))
			return
		}

		switch settingsVersion {
		case "1.4.4":
			// Set UUID Value in xepg.json
			err = setValueForUUID()
			if err != nil {
				return
			}

			// New filter (WebUI). Old Filter Settings are converted
			if oldFilter, ok := settingsMap["filter"].([]any); ok {
				var newFilterMap = convertToNewFilter(oldFilter)
				settingsMap["filter"] = newFilterMap

				settingsMap["version"] = "2.0.0"

				err = saveMapToJSONFile(System.File.Settings, settingsMap)
				if err != nil {
					return
				}
				goto checkVersion
			} else {
				err = errors.New(getErrMsg(1030))
				return
			}
		case "2.0.0":
			if oldBuffer, ok := settingsMap["buffer"].(bool); ok {
				var newBuffer string
				switch oldBuffer {
				case true:
					newBuffer = "xteve"
				case false:
					newBuffer = "-"
				}

				settingsMap["buffer"] = newBuffer

				settingsMap["version"] = "2.1.0"

				err = saveMapToJSONFile(System.File.Settings, settingsMap)
				if err != nil {
					return
				}
				goto checkVersion
			} else {
				err = errors.New(getErrMsg(1030))
				return
			}
		case "2.1.0", "2.1.1":
			// Database verison <= 2.1.1 has broken XEPG mapping

			// Clear XEPG mapping
			Data.XEPG.Channels = make(map[string]any)
			Data.XEPG.XEPGCount = 0
			Data.Cache.Streams = struct{ Active []string }{}

			err = saveMapToJSONFile(System.File.XEPG, Data.XEPG.Channels)
			if err != nil {
				ShowError(err, 000)
				return err
			}

			// Notify user
			showWarning(2022)
			sendAlert(getErrMsg(2022))

			// Update database version
			settingsMap["version"] = "2.2.0"

			err = saveMapToJSONFile(System.File.Settings, settingsMap)
			if err != nil {
				return
			}
			goto checkVersion
		case "2.2.0", "2.2.1", "2.2.2", "2.2.3", "2.3.0":
			// If there are changes to the Database in a later update, continue here
			break
		}
	} else {
		// settings.json is too old (older than Version 1.4.4)
		err = errors.New(getErrMsg(1013))
	}
	return
}

func convertToNewFilter(oldFilter []any) (newFilterMap map[int]any) {
	newFilterMap = make(map[int]any)

	switch reflect.TypeOf(oldFilter).Kind() {
	case reflect.Slice:
		s := reflect.ValueOf(oldFilter)

		for i := range s.Len() {
			var newFilter FilterStruct
			newFilter.Active = true
			newFilter.Name = fmt.Sprintf("Custom filter %d", i+1)
			if filter, ok := s.Index(i).Interface().(string); ok {
				newFilter.Filter = filter
			}
			newFilter.Type = "custom-filter"
			newFilter.CaseSensitive = false

			newFilterMap[i] = newFilter
		}
	}
	return
}

func setValueForUUID() (err error) {
	xepg, _ := loadJSONFileToMap(System.File.XEPG)

	for _, c := range xepg {
		var xepgChannel, ok = c.(map[string]any)
		if !ok {
			continue
		}

		if uuidKey, ok := xepgChannel["_uuid.key"].(string); ok {
			if value, ok := xepgChannel[uuidKey].(string); ok {
				if len(value) > 0 {
					xepgChannel["_uuid.value"] = value
				}
			}
		}
	}

	err = saveMapToJSONFile(System.File.XEPG, xepg)
	return
}
