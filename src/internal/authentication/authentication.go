package authentication

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const tokenLength = 40
const saltLength = 20
const idLength = 10

var tokenValidity int
var database string

var databaseFile = "authentication.json"

var data = make(map[string]any)
var tokens = make(map[string]any)

var initAuthentication = false

// Init : databasePath = Path to authentication.json
func Init(databasePath string, validity int) (err error) {
	database = filepath.Dir(databasePath) + string(os.PathSeparator) + databaseFile

	// Check if the database already exists
	if _, err = os.Stat(database); os.IsNotExist(err) {
		// Create an empty database
		var defaults = make(map[string]any)
		defaults["dbVersion"] = "1.0"
		defaults["hash"] = "sha256"
		defaults["users"] = make(map[string]any)

		if saveDatabase(defaults) != nil {
			return
		}
	}

	// Loading the database
	err = loadDatabase()

	// Set Token Validity
	tokenValidity = validity
	initAuthentication = true
	return
}

// CreateDefaultUser = created efault user
func CreateDefaultUser(username, password string) (err error) {
	err = checkInit()
	if err != nil {
		return
	}

	var users, ok = data["users"].(map[string]any)
	if !ok {
		return createError(001)
	}

	// Check if the default user exists
	if len(users) > 0 {
		err = createError(001)
		return
	}

	defaults, err := defaultsForNewUser(username, password)
	if err != nil {
		return
	}

	var defaultID, okID = defaults["_id"].(string)
	if !okID {
		return errors.New("default user has no ID")
	}

	users[defaultID] = defaults
	err = saveDatabase(data)
	return
}

// CreateNewUser : create new user
func CreateNewUser(username, password string) (userID string, err error) {
	err = checkInit()
	if err != nil {
		return
	}

	var checkIfTheUserAlreadyExists = func(username string, iUserData any) (err error) {
		var userData, ok = iUserData.(map[string]any)
		if !ok {
			return errors.New("user data has to be a map")
		}

		var salt, okSalt = userData["_salt"].(string)
		if !okSalt {
			return errors.New("user has no salt")
		}

		var loginUsername, okLogin = userData["_username"].(string)
		if !okLogin {
			return errors.New("user has no username")
		}

		sUsername, err := SHA256(username, salt)
		if err != nil {
			return err
		}

		if sUsername == loginUsername {
			err = createError(020)
			return
		}

		// Check legacy hash to prevent duplicates during migration
		sUsernameLegacy, errLegacy := legacySHA256(username, salt)
		if errLegacy != nil {
			err = errLegacy
			return
		}

		if sUsernameLegacy == loginUsername {
			err = createError(020)
			return
		}

		return
	}

	var users, ok = data["users"].(map[string]any)
	if !ok {
		return "", errors.New("users in datebase are not a map")
	}

	for _, userData := range users {
		err = checkIfTheUserAlreadyExists(username, userData)
		if err != nil {
			return
		}
	}

	defaults, err := defaultsForNewUser(username, password)
	if err != nil {
		return
	}

	userID, ok = defaults["_id"].(string)
	if !ok {
		return "", errors.New("default user has no ID")
	}

	users[userID] = defaults

	err = saveDatabase(data)
	return
}

// UserAuthentication : user authentication
func UserAuthentication(username, password string) (token string, err error) {
	err = checkInit()
	if err != nil {
		return
	}

	var login = func(username, password string, iLoginData any) (err error) {
		err = createError(010)

		var loginData, ok = iLoginData.(map[string]any)
		if !ok {
			return errors.New("user data has to be a map")
		}

		var salt, okSalt = loginData["_salt"].(string)
		if !okSalt {
			return errors.New("user has no salt")
		}

		var loginUsername, okLogin = loginData["_username"].(string)
		if !okLogin {
			return errors.New("user has no username")
		}

		var loginPassword, okPass = loginData["_password"].(string)
		if !okPass {
			return errors.New("user has no password")
		}

		// Try NEW hash first
		sUsername, errSHA := SHA256(username, salt)
		if errSHA != nil {
			return errSHA
		}
		sPassword, errSHA := SHA256(password, salt)
		if errSHA != nil {
			return errSHA
		}

		if sUsername == loginUsername {
			if sPassword == loginPassword {
				err = nil
				return
			}
		}

		// Try LEGACY hash
		sUsernameLegacy, errSHA := legacySHA256(username, salt)
		if errSHA != nil {
			return errSHA
		}
		sPasswordLegacy, errSHA := legacySHA256(password, salt)
		if errSHA != nil {
			return errSHA
		}

		if sUsernameLegacy == loginUsername {
			if sPasswordLegacy == loginPassword {
				// Legacy Match! Migrate to new hash.
				loginData["_username"] = sUsername
				loginData["_password"] = sPassword

				if errSave := saveDatabase(data); errSave != nil {
					// If save fails, we still allow login, but log/return error?
					// For now, allow login.
				}
				err = nil
			}
		}
		return
	}

	var users, ok = data["users"].(map[string]any)
	if !ok {
		return "", errors.New("users in datebase are not a map")
	}

	for id, loginData := range users {
		err = login(username, password, loginData)
		if err == nil {
			token, err = setToken(id, "-")
			return
		}
	}
	return
}

// CheckTheValidityOfTheToken : check token
func CheckTheValidityOfTheToken(token string) (newToken string, err error) {
	err = checkInit()
	if err != nil {
		return
	}

	err = createError(011)

	if v, ok := tokens[token]; ok {
		var tokenData, okToken = v.(map[string]any)
		if !okToken {
			return "", errors.New("token data has to be a map")
		}

		var expires, okExpires = tokenData["expires"].(time.Time)
		if !okExpires {
			return "", errors.New("token has no expiration date")
		}

		var userID, okID = tokenData["id"].(string)
		if !okID {
			return "", errors.New("token has no ID")
		}

		if expires.Sub(time.Now().Local()) < 0 {
			return
		}
		newToken, err = setToken(userID, token)
		if err != nil {
			return "", err // Propagate error from setToken
		}
		err = nil
	} else {
		// err is already createError(011) from the top of the function
		return "", err
	}
	return
}

// GetUserID : get user ID
func GetUserID(token string) (userID string, err error) {
	err = checkInit()
	if err != nil {
		return
	}

	err = createError(002)

	if v, ok := tokens[token]; ok {
		var tokenData, okToken = v.(map[string]any)
		if !okToken {
			return "", errors.New("token data has to be a map")
		}

		var expires, okExpires = tokenData["expires"].(time.Time)
		if !okExpires {
			return "", errors.New("token has no expiration date")
		}

		userID, ok = tokenData["id"].(string)
		if !ok {
			return "", errors.New("token has no ID")
		}

		if expires.Sub(time.Now().Local()) < 0 {
			return
		}
		err = nil
	}
	return
}

// WriteUserData : save user date
func WriteUserData(userID string, userData map[string]any) (err error) {
	err = checkInit()
	if err != nil {
		return
	}

	err = createError(030)

	if v, ok := data["users"].(map[string]any)[userID].(map[string]any); ok {
		v["data"] = userData
		err = saveDatabase(data)
	} else {
		return
	}
	return
}

// ReadUserData : load user date
func ReadUserData(userID string) (userData map[string]any, err error) {
	err = checkInit()
	if err != nil {
		return
	}

	err = createError(031)

	if v, ok := data["users"].(map[string]any)[userID].(map[string]any); ok {
		userData, ok = v["data"].(map[string]any)
		if !ok {
			return nil, errors.New("user data is not a map")
		}
		err = nil
		return
	}
	return
}

// RemoveUser : remove user
func RemoveUser(userID string) (err error) {
	err = checkInit()
	if err != nil {
		return
	}

	err = createError(032)
	var users, ok = data["users"].(map[string]any)
	if !ok {
		return errors.New("users in datebase are not a map")
	}

	if _, ok := users[userID]; ok {
		delete(users, userID)
		err = saveDatabase(data)
		return
	}
	return
}

// SetDefaultUserData : set default user data
func SetDefaultUserData(defaults map[string]any) (err error) {
	allUserData, err := GetAllUserData()
	if err != nil {
		return err // Propagate error from GetAllUserData
	}

	for _, d := range allUserData {
		var user, okUser = d.(map[string]any)
		if !okUser {
			return errors.New("user data has to be a map")
		}

		var userDataMap, okData = user["data"].(map[string]any) // Renamed to avoid conflict
		if !okData {
			return errors.New("data in user data has to be a map")
		}

		var userID, okID = user["_id"].(string)
		if !okID {
			return errors.New("user has no ID")
		}

		for k, v := range defaults {
			if _, ok := userDataMap[k]; ok {
				// Key exist
			} else {
				userDataMap[k] = v
			}
		}
		err = WriteUserData(userID, userDataMap)
		if err != nil {
			return err // Propagate error from WriteUserData
		}
	}
	return
}

// ChangeCredentials : change credentials
func ChangeCredentials(userID, username, password string) (err error) {
	err = checkInit()
	if err != nil {
		return
	}

	// err = createError(032) // Removed ineffectual assignment

	errCreate := createError(032) // Keep original error in case user not found

	var users, ok = data["users"].(map[string]any)
	if !ok {
		return errors.New("users in datebase are not a map")
	}

	if iUserData, ok := users[userID]; ok {
		var userData, okUser = iUserData.(map[string]any)
		if !okUser {
			return errors.New("user data has to be a map")
		}

		var salt string
		salt, ok = userData["_salt"].(string)
		if !ok {
			return errors.New("user has no salt")
		}

		if len(username) > 0 {
			usernameHash, errSHA := SHA256(username, salt)
			if errSHA != nil {
				return errSHA
			}
			userData["_username"] = usernameHash
		}

		if len(password) > 0 {
			passwordHash, errSHA := SHA256(password, salt)
			if errSHA != nil {
				return errSHA
			}
			userData["_password"] = passwordHash
		}
		err = saveDatabase(data)
		if err != nil {
			return err // Propagate error from saveDatabase
		}
		return nil // Successful update, clear original error
	}
	err = errCreate // User not found, return the original error
	return
}

// GetAllUserData : get all user data
func GetAllUserData() (allUserData map[string]any, err error) {
	err = checkInit()
	if err != nil {
		return
	}

	if len(data) == 0 {
		var defaults = make(map[string]any)
		defaults["dbVersion"] = "1.0"
		defaults["hash"] = "sha256"
		defaults["users"] = make(map[string]any)
		err = saveDatabase(defaults) // Handle error from saveDatabase
		if err != nil {
			return nil, err
		}
		data = defaults
	}

	var ok bool
	allUserData, ok = data["users"].(map[string]any)
	if !ok {
		return nil, errors.New("users in datebase are not a map")
	}

	return
}

// CheckTheValidityOfTheTokenFromHTTPHeader : get token from HTTP header
func CheckTheValidityOfTheTokenFromHTTPHeader(w http.ResponseWriter, r *http.Request) (writer http.ResponseWriter, newToken string, err error) {
	err = createError(011) // Default error if token is not found or invalid
	for _, cookie := range r.Cookies() {
		if cookie.Name == "Token" {
			var currentTokenValue = cookie.Value
			token, errCheck := CheckTheValidityOfTheToken(currentTokenValue) // Use new var for this error
			if errCheck != nil {
				// If token is invalid, we might want to clear it or just report error
				// For now, let's propagate the error and not set a new cookie
				err = errCheck // Assign the specific error from CheckTheValidityOfTheToken
				return writer, "", err
			}
			writer = SetCookieToken(w, token)
			newToken = token
			err = nil // Token was valid and processed, clear default error
			return    // Found and processed the token, no need to check other cookies
		}
	}
	// If loop finishes, means no "Token" cookie was found.
	// The initial err = createError(011) will be returned.
	return
}

// Framework tools

func checkInit() (err error) {
	if !initAuthentication {
		err = createError(000)
	}
	return
}

func saveDatabase(tmpMap any) (err error) {
	jsonString, err := json.MarshalIndent(tmpMap, "", "  ")

	if err != nil {
		return
	}

	err = os.WriteFile(database, []byte(jsonString), 0600)
	if err != nil {
		return
	}
	return
}

func loadDatabase() (err error) {
	jsonString, err := os.ReadFile(database)
	if err != nil {
		return
	}

	err = json.Unmarshal([]byte(jsonString), &data)
	if err != nil {
		return
	}
	return
}

// SHA256 : password + salt = sha256 string
// Fixed to use salt.
func SHA256(secret, salt string) (string, error) {
	key := []byte(secret)
	h := hmac.New(sha256.New, key)
	_, err := h.Write([]byte(salt))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(h.Sum(nil)), nil
}

// legacySHA256 preserves the old insecure hashing for migration purposes
func legacySHA256(secret, salt string) (string, error) {
	key := []byte(secret)
	h := hmac.New(sha256.New, key)
	_, err := h.Write([]byte("_remote_db"))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(h.Sum(nil)), nil
}

func randomString(n int) (string, error) {
	const alphanum = "-AbCdEfGhIjKlMnOpQrStUvWxYz0123456789aBcDeFgHiJkLmNoPqRsTuVwXyZ_"

	var bytes = make([]byte, n)
	_, err := rand.Read(bytes)
	if err != nil {
		return "", err
	}
	for i, b := range bytes {
		bytes[i] = alphanum[b%byte(len(alphanum))]
	}
	return string(bytes), nil
}

func randomID(n int) (string, error) {
	const alphanum = "ABCDEFGHJKLMNOPQRSTUVWXYZ0123456789"

	var bytes = make([]byte, n)
	_, err := rand.Read(bytes)
	if err != nil {
		return "", err
	}
	for i, b := range bytes {
		bytes[i] = alphanum[b%byte(len(alphanum))]
	}
	return string(bytes), nil
}

func createError(errCode int) (err error) {
	var errMsg string
	switch errCode {
	case 000:
		errMsg = "Authentication has not yet been initialized"
	case 001:
		errMsg = "Default user already exists"
	case 002:
		errMsg = "No user id found for this token"
	case 010:
		errMsg = "User authentication failed"
	case 011:
		errMsg = "Session has expired"
	case 020:
		errMsg = "User already exists"
	case 030:
		errMsg = "User data could not be saved"
	case 031:
		errMsg = "User data could not be read"
	case 032:
		errMsg = "User ID was not found"
	}

	err = errors.New(errMsg)
	return
}

func defaultsForNewUser(username, password string) (map[string]any, error) {
	var defaults = make(map[string]any)
	salt, err := randomString(saltLength)
	if err != nil {
		return nil, err
	}
	usernameHash, err := SHA256(username, salt)
	if err != nil {
		return nil, err
	}
	passwordHash, err := SHA256(password, salt)
	if err != nil {
		return nil, err
	}
	idSuffix, err := randomID(idLength)
	if err != nil {
		return nil, err
	}
	defaults["_username"] = usernameHash
	defaults["_password"] = passwordHash
	defaults["_salt"] = salt
	defaults["_id"] = "id-" + idSuffix
	defaults["data"] = make(map[string]any)
	return defaults, nil
}

func setToken(id, oldToken string) (newToken string, err error) {
	delete(tokens, oldToken)
loopToken:
	newToken, err = randomString(tokenLength)
	if err != nil {
		return "", err
	}
	if _, ok := tokens[newToken]; ok {
		goto loopToken
	}

	var tmp = make(map[string]any)
	tmp["id"] = id
	tmp["expires"] = time.Now().Local().Add(time.Minute * time.Duration(tokenValidity))

	tokens[newToken] = tmp
	return
}

// SetCookieToken : set cookie
func SetCookieToken(w http.ResponseWriter, token string) http.ResponseWriter {
	expiration := time.Now().Add(time.Minute * time.Duration(tokenValidity))
	cookie := http.Cookie{Name: "Token", Value: token, Expires: expiration}
	http.SetCookie(w, &cookie)
	return w
}
