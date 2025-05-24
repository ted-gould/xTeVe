package src

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strings"

	"xteve/src/internal/authentication"
)

func activatedSystemAuthentication() (err error) {
	err = authentication.Init(System.Folder.Config, 60)
	if err != nil {
		return
	}

	var defaults = make(map[string]any)
	defaults["authentication.web"] = false
	defaults["authentication.pms"] = false
	defaults["authentication.xml"] = false
	defaults["authentication.api"] = false
	err = authentication.SetDefaultUserData(defaults)
	// Propagate error from SetDefaultUserData
	return
}

func createFirstUserForAuthentication(username, password string) (token string, err error) {
	err = authentication.CreateDefaultUser(username, password)
	if err != nil {
		return "", err
	}

	token, err = authentication.UserAuthentication(username, password)
	if err != nil {
		return "", err
	}

	token, err = authentication.CheckTheValidityOfTheToken(token)
	if err != nil {
		return "", err
	}

	var userData = make(map[string]any)
	userData["username"] = username
	userData["authentication.web"] = true
	userData["authentication.pms"] = true
	userData["authentication.m3u"] = true
	userData["authentication.xml"] = true
	userData["authentication.api"] = false
	userData["defaultUser"] = true

	userID, err := authentication.GetUserID(token)
	if err != nil {
		return "", err
	}

	err = authentication.WriteUserData(userID, userData)
	if err != nil {
		return "", err
	}

	return
}

func tokenAuthentication(token string) (newToken string, err error) {
	if System.ConfigurationWizard {
		return
	}

	newToken, err = authentication.CheckTheValidityOfTheToken(token)

	return
}

func basicAuth(r *http.Request, level string) (username string, err error) {
	err = errors.New("user authentication failed")

	auth := strings.SplitN(r.Header.Get("Authorization"), " ", 2)

	if len(auth) != 2 || auth[0] != "Basic" {
		return
	}

	payload, errDecode := base64.StdEncoding.DecodeString(auth[1])
	if errDecode != nil {
		// If decoding fails, it's an invalid Authorization header.
		// The original err (user authentication failed) is appropriate.
		return "", err // Return the original error
	}
	pair := strings.SplitN(string(payload), ":", 2)

	if len(pair) != 2 {
		// If not two parts, it's an invalid format.
		return "", err // Return the original error
	}

	username = pair[0]
	var password = pair[1]

	token, err := authentication.UserAuthentication(username, password)

	if err != nil {
		return
	}

	err = checkAuthorizationLevel(token, level)

	return
}

func urlAuth(r *http.Request, requestType string) (err error) {
	var level, token string

	var username = r.URL.Query().Get("username")
	var password = r.URL.Query().Get("password")

	switch requestType {
	case "m3u":
		level = "authentication.m3u"
		if Settings.AuthenticationM3U {
			token, err = authentication.UserAuthentication(username, password)
			if err != nil {
				return
			}
			err = checkAuthorizationLevel(token, level)
		}

	case "xml":
		level = "authentication.xml"
		if Settings.AuthenticationXML {
			token, err = authentication.UserAuthentication(username, password)
			if err != nil {
				return
			}
			err = checkAuthorizationLevel(token, level)
		}
	}

	return
}

func checkAuthorizationLevel(token, level string) (err error) {
	var authenticationErr = func(err error) {
		if err != nil {
			return
		}
	}

	userID, err := authentication.GetUserID(token)
	authenticationErr(err)

	userData, err := authentication.ReadUserData(userID)
	authenticationErr(err)

	if len(userData) > 0 {
		if v, ok := userData[level].(bool); ok {
			if !v {
				err = errors.New("no authorization")
			}
		} else {
			// Level not found, set to false and try to save.
			// The user does not have authorization regardless of save success.
			userData[level] = false
			if writeErr := authentication.WriteUserData(userID, userData); writeErr != nil {
				// Log the error, but the primary error (no authorization) stands.
				// log.Printf("Failed to write default authorization level for user %s, level %s: %v", userID, level, writeErr)
			}
			err = errors.New("no authorization") // Ensure error is set if level was not found
		}
	} else {
		// UserData is empty, this is an unusual case.
		// Attempt to write, but the user definitely doesn't have authorization.
		if writeErr := authentication.WriteUserData(userID, userData); writeErr != nil {
			// log.Printf("Failed to write empty userData for user %s, level %s: %v", userID, level, writeErr)
		}
		err = errors.New("no authorization") // Ensure error is set if userData was empty
	}

	return
}
