package authentication

import (
	"encoding/json"
	"errors"
	// "io/ioutil" // Will be removed
	"net/http"
	"os"
	"path/filepath"

	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"

	"time"
	//"fmt"
	//"log"
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

// Cookie : cookie
type Cookie struct {
	Name       string
	Value      string
	Path       string
	Domain     string
	Expires    time.Time
	RawExpires string
}

// Framework examples

/*
func main() {
  var err error

  var checkErr = func(err error) {
     log.Println(err)
     os.Exit(0)
  }

  err = Init("", 10)          // Path to save the data, Validity of tokens in minutes | (error)
  if err != nil {
    checkErr(err)
  }


  err = CreateDefaultUser("admin", "123")
  if err != nil {
    checkErr(err)
  }




  err = CreateNewUser("xteve", "xteve")          // Username, Password | (error)
  if err != nil {
    checkErr(err)
  }



  err, token := UserAuthentication("xteve", "xteve")          // Username, Password | (error, token)
  if err != nil {
    checkErr(err)
  } else {
    fmt.Println("UserAuthentication()")
    fmt.Println("Token:", token)
    fmt.Println("---")
  }

  err, newToken := CheckTheValidityOfTheToken(token)        // Current token | (error, new token)
  if err != nil {
    checkErr(err)
  } else {
    fmt.Println("CheckTheValidityOfTheToken()")
    fmt.Println("New Token:", newToken)
    fmt.Println("---")
  }

  err, userID := GetUserID(newToken)                        // Current token | (error, user id)
  if err != nil {
    checkErr(err)
  } else {
    fmt.Println("GetUserID()")
    fmt.Println("User ID:", userID)
    fmt.Println("---")
  }


  var userData = make(map[string]interface{})
  userData["type"] = "Administrator"
  err = WriteUserData(userID, userData)          // User id, user data | (error)
  if err != nil {
    checkErr(err)
  }

  err, userData = ReadUserData(userID)          // User id | (error, userData)
  if err != nil {
    checkErr(err)
  } else {
    fmt.Println("ReadUserData()")
    fmt.Println("User data:", userData)
    fmt.Println("---")
  }

  err = RemoveUser(userID)
  if err != nil {
    checkErr(err)
  }

}
*/

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

	var users = data["users"].(map[string]any)
	// Check if the default user exists
	if len(users) > 0 {
		err = createError(001)
		return
	}

	defaults, err := defaultsForNewUser(username, password)
	if err != nil {
		return
	}
	users[defaults["_id"].(string)] = defaults
	err = saveDatabase(data)
	return
}

// CreateNewUser : create new user
func CreateNewUser(username, password string) (userID string, err error) {
	err = checkInit()
	if err != nil {
		return
	}

	var checkIfTheUserAlreadyExists = func(username string, userData map[string]any) (err error) {
		var salt = userData["_salt"].(string)
		var loginUsername = userData["_username"].(string)

		sUsername, err := SHA256(username, salt)
		if err != nil {
			return err
		}

		if sUsername == loginUsername {
			err = createError(020)
		}
		return
	}

	var users = data["users"].(map[string]any)
	for _, userData := range users {
		err = checkIfTheUserAlreadyExists(username, userData.(map[string]any))
		if err != nil {
			return
		}
	}

	defaults, err := defaultsForNewUser(username, password)
	if err != nil {
		return
	}
	userID = defaults["_id"].(string)
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

	var login = func(username, password string, loginData map[string]any) (err error) {
		err = createError(010)

		var salt = loginData["_salt"].(string)
		var loginUsername = loginData["_username"].(string)
		var loginPassword = loginData["_password"].(string)

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
			}
		}
		return
	}

	var users = data["users"].(map[string]any)
	for id, loginData := range users {
		err = login(username, password, loginData.(map[string]any))
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
		var expires = v.(map[string]any)["expires"].(time.Time)
		var userID = v.(map[string]any)["id"].(string)

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
		var expires = v.(map[string]any)["expires"].(time.Time)
		userID = v.(map[string]any)["id"].(string)

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
		userData = v["data"].(map[string]any)
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

	if _, ok := data["users"].(map[string]any)[userID]; ok {
		delete(data["users"].(map[string]any), userID)
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
		var userDataMap = d.(map[string]any)["data"].(map[string]any) // Renamed to avoid conflict
		var userID = d.(map[string]any)["_id"].(string)

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

	if userData, ok := data["users"].(map[string]any)[userID]; ok {
		//var userData = tmp.(map[string]interface{})
		var salt = userData.(map[string]any)["_salt"].(string)

		if len(username) > 0 {
			usernameHash, errSHA := SHA256(username, salt)
			if errSHA != nil {
				return errSHA
			}
			userData.(map[string]any)["_username"] = usernameHash
		}

		if len(password) > 0 {
			passwordHash, errSHA := SHA256(password, salt)
			if errSHA != nil {
				return errSHA
			}
			userData.(map[string]any)["_password"] = passwordHash
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

	allUserData = data["users"].(map[string]any)
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
			//fmt.Println("T", token, errCheck)
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
func SHA256(secret, salt string) (string, error) {
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
	//defaults["_one.time.token"], err = randomString(tokenLength)
	//if err != nil {
	//	return nil, err
	//}
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
