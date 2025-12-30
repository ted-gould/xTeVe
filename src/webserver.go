package src

import (
	"context"
	"encoding/json"
	"mime"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log" // Added for log.Printf
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"xteve/src/internal/authentication"

	"github.com/gorilla/websocket"
	"github.com/samber/lo"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/net/webdav"
)

// webAlerts channel to send to client
var webAlerts = make(chan string, 3)
var restartWebserver = make(chan bool, 1)

func init() {
	// Fix for MIME type issue with .js files
	_ = mime.AddExtensionType(".js", "application/javascript")
}

// StartWebserver : Start the Webserver
func StartWebserver() (err error) {
	for {
		showInfo("Web server:" + "Starting")

		showInfo("DVR IP:" + Settings.HostIP + ":" + Settings.Port)

		var ips = len(System.IPAddressesV4) + len(System.IPAddressesV6) - 1
		switch ips {
		case 0:
			showHighlight(fmt.Sprintf("Web Interface:%s://%s:%s/web/", System.ServerProtocol.WEB, Settings.HostIP, Settings.Port))
		case 1:
			showHighlight(fmt.Sprintf("Web Interface:%s://%s:%s/web/ | xTeVe is also available via the other %d IP.", System.ServerProtocol.WEB, Settings.HostIP, Settings.Port, ips))
		default:
			showHighlight(fmt.Sprintf("Web Interface:%s://%s:%s/web/ | xTeVe is also available via the other %d IP's.", System.ServerProtocol.WEB, Settings.HostIP, Settings.Port, len(System.IPAddressesV4)+len(System.IPAddressesV6)-1))
		}

		var port = Settings.Port
		server := http.Server{Addr: ":" + port, Handler: newHTTPHandler()}

		go func() {
			var err error

			if Settings.TLSMode {
				if !allFilesExist(System.File.ServerCertPrivKey, System.File.ServerCert) {
					if err = genCertFiles(); err != nil {
						ShowError(err, 7000)
					}
				}

				err = server.ListenAndServeTLS(System.File.ServerCert, System.File.ServerCertPrivKey)
				if err != nil && err != http.ErrServerClosed {
					ShowError(err, 1017)
					err = server.ListenAndServe()
				}
			} else {
				err = server.ListenAndServe()
			}

			if err != nil && err != http.ErrServerClosed {
				ShowError(err, 1001)
				return
			}
		}()

		<-restartWebserver
		showInfo("Web server:" + "Restarting")

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err = server.Shutdown(ctx); err != nil {
			ShowError(err, 1016)
			return
		}

		<-ctx.Done()
		showInfo("Web server:" + "Stopped")
	}
}

// Index : Web Server /
func Index(w http.ResponseWriter, r *http.Request) {
	var err error
	var response []byte
	var path = r.URL.Path
	var debug string

	setGlobalDomain(r.Host)

	debug = fmt.Sprintf("Web Server Request:Path: %s", path)
	showDebug(debug, 2)

	switch path {
	case "/favicon.ico":
		_, childSpan := otel.Tracer("webserver").Start(r.Context(), "favicon")
		defer childSpan.End()
		response, err = webUI.ReadFile("favicon.ico")
		if err != nil {
			childSpan.RecordError(err)
			httpStatusError(w, r, 404)
			return
		}
		w.Header().Set("Content-Type", "image/x-icon")
		w.WriteHeader(200)
		if _, writeErr := w.Write(response); writeErr != nil {
			log.Printf("Error writing response in Index handler: %v", writeErr)
		}
		return
	case "/discover.json":
		_, childSpan := otel.Tracer("webserver").Start(r.Context(), "discover")
		defer childSpan.End()
		response, err = getDiscover()
		if err != nil {
			childSpan.RecordError(err)
		}
		w.Header().Set("Content-Type", "application/json")
	case "/lineup_status.json":
		_, childSpan := otel.Tracer("webserver").Start(r.Context(), "lineup_status")
		defer childSpan.End()
		response, err = getLineupStatus()
		if err != nil {
			childSpan.RecordError(err)
		}
		w.Header().Set("Content-Type", "application/json")
	case "/lineup.json":
		_, childSpan := otel.Tracer("webserver").Start(r.Context(), "lineup")
		defer childSpan.End()
		if Settings.AuthenticationPMS {
			_, err := basicAuth(r, "authentication.pms")
			if err != nil {
				childSpan.RecordError(err)
				ShowError(err, 000)
				httpStatusError(w, r, 403)
				return
			}
		}
		response, err = getLineup()
		if err != nil {
			childSpan.RecordError(err)
		}
		w.Header().Set("Content-Type", "application/json")
	case "/device.xml", "/capability":
		_, childSpan := otel.Tracer("webserver").Start(r.Context(), "capability")
		defer childSpan.End()
		response, err = getCapability()
		if err != nil {
			childSpan.RecordError(err)
		}
		w.Header().Set("Content-Type", "application/xml")
	default:
		_, childSpan := otel.Tracer("webserver").Start(r.Context(), "default")
		defer childSpan.End()
		response, err = getCapability()
		if err != nil {
			childSpan.RecordError(err)
		}
		w.Header().Set("Content-Type", "application/xml")
	}

	if err == nil {
		w.WriteHeader(200)
		if _, writeErr := w.Write(response); writeErr != nil {
			log.Printf("Error writing response in Index handler: %v", writeErr)
			// At this point, headers are sent, so we can't send a different HTTP error.
		}
		return
	}
	httpStatusError(w, r, 500)
}

// Stream : Web Server /stream/
func Stream(w http.ResponseWriter, r *http.Request) {
	var path = strings.Replace(r.RequestURI, "/stream/", "", 1)
	//var stream = strings.SplitN(path, "-", 2)

	streamInfo, err := getStreamInfo(path)
	if err != nil {
		trace.SpanFromContext(r.Context()).RecordError(err)
		ShowError(err, 1203)
		httpStatusError(w, r, 404)
		return
	}

	// If an UDPxy host is set, and the stream URL is multicast (i.e. starts with 'udp://@'),
	// then streamInfo.URL needs to be rewritten to point to UDPxy.
	if Settings.UDPxy != "" && strings.HasPrefix(streamInfo.URL, "udp://@") {
		streamInfo.URL = fmt.Sprintf("http://%s/udp/%s/", Settings.UDPxy, strings.TrimPrefix(streamInfo.URL, "udp://@"))
	}

	switch Settings.Buffer {
	case "-":
		showInfo(fmt.Sprintf("Buffer:false [%s]", Settings.Buffer))
	case "xteve":
		if strings.Contains(streamInfo.URL, "rtsp://") || strings.Contains(streamInfo.URL, "rtp://") {
			err = errors.New("RTSP and RTP streams are not supported")
			ShowError(err, 2004)

			showInfo("Streaming URL:" + streamInfo.URL)
			http.Redirect(w, r, streamInfo.URL, http.StatusFound)

			showInfo("Streaming Info:URL was passed to the client")
			return
		}
		showInfo(fmt.Sprintf("Buffer:true [%s]", Settings.Buffer))
	default:
		showInfo(fmt.Sprintf("Buffer:true [%s]", Settings.Buffer))
	}

	if Settings.Buffer != "-" {
		showInfo(fmt.Sprintf("Buffer Size:%d KB", Settings.BufferSize))
	}

	showInfo(fmt.Sprintf("Channel Name:%s", streamInfo.Name))
	showInfo(fmt.Sprintf("Client User-Agent:%s", r.Header.Get("User-Agent")))

	// Check whether the Buffer should be used
	switch Settings.Buffer {
	case "-":
		showInfo("Streaming URL:" + streamInfo.URL)
		http.Redirect(w, r, streamInfo.URL, http.StatusFound)

		showInfo("Streaming Info:URL was passed to the client.")
		showInfo("Streaming Info:xTeVe is no longer involved, the client connects directly to the streaming server.")
	default:
		bufferingStream(streamInfo.PlaylistID, streamInfo.URL, streamInfo.Name, w, r)
	}
}

// Auto : HDHR routing (is currently not used)
func Auto(w http.ResponseWriter, r *http.Request) {
	var channelID = strings.Replace(r.RequestURI, "/auto/v", "", 1)
	fmt.Println(channelID)

	/*
		switch Settings.Buffer {

		case true:
			var playlistID, streamURL, err = getStreamByChannelID(channelID)
			if err == nil {
				bufferingStream(playlistID, streamURL, w, r)
			} else {
				httpStatusError(w, r, 404)
			}

		case false:
			httpStatusError(w, r, 423)
		}
	*/
}

// xTeVe : Web Server /xmltv/ and /m3u/
func xTeVe(w http.ResponseWriter, r *http.Request) {
	var requestType, groupTitle, file, content, contentType string
	var err error
	var path = strings.TrimPrefix(r.URL.Path, "/")
	var groups = []string{}

	setGlobalDomain(r.Host)

	// XMLTV File
	if strings.Contains(path, "xmltv/") {
		_, childSpan := otel.Tracer("webserver").Start(r.Context(), "xmltv")
		defer childSpan.End()

		requestType = "xml"
		file = System.Folder.Data + getFilenameFromPath(path)
		content, err = readStringFromFile(file)
		if err != nil {
			childSpan.RecordError(err)
			httpStatusError(w, r, 404)
			return
		}
	}

	// M3U File
	if strings.Contains(path, "m3u/") {
		_, childSpan := otel.Tracer("webserver").Start(r.Context(), "m3u")
		defer childSpan.End()

		requestType = "m3u"
		groupTitle = r.URL.Query().Get("group-title")

		if !System.Dev {
			// false: File name is set in the header
			// true: M3U is displayed directly in the browser
			w.Header().Set("Content-Disposition", "attachment; filename="+getFilenameFromPath(path))
		}

		if len(groupTitle) > 0 {
			groups = strings.Split(groupTitle, ",")
		}

		content, err = buildM3U(groups)
		if err != nil {
			childSpan.RecordError(err)
			ShowError(err, 000)
		}
	}

	// Check Authentication
	err = urlAuth(r, requestType)
	if err != nil {
		ShowError(err, 000)
		httpStatusError(w, r, 403)
		return
	}

	contentType = http.DetectContentType([]byte(content))
	if strings.Contains(strings.ToLower(contentType), "xml") {
		contentType = "application/xml; charset=utf-8"
	}

	w.Header().Set("Content-Type", contentType)
	if _, writeErr := w.Write([]byte(content)); writeErr != nil {
		log.Printf("Error writing response in xTeVe handler: %v", writeErr)
	}
}

// Images : Image Cache /images/
func Images(w http.ResponseWriter, r *http.Request) {
	var path = strings.TrimPrefix(r.URL.Path, "/")
	var filePath = System.Folder.ImagesCache + getFilenameFromPath(path)

	content, err := readByteFromFile(filePath)
	if err != nil {
		trace.SpanFromContext(r.Context()).RecordError(err)
		httpStatusError(w, r, 404)
		return
	}

	w.Header().Add("Content-Type", getContentType(filePath))
	w.Header().Add("Content-Length", fmt.Sprintf("%d", len(content)))
	w.WriteHeader(200)
	if _, writeErr := w.Write(content); writeErr != nil {
		log.Printf("Error writing image response in Images handler: %v", writeErr)
	}
}

// DataImages : Image path for Logos / Images that have been uploaded / data_images /
func DataImages(w http.ResponseWriter, r *http.Request) {
	var path = strings.TrimPrefix(r.URL.Path, "/")
	var filePath = System.Folder.ImagesUpload + getFilenameFromPath(path)

	content, err := readByteFromFile(filePath)
	if err != nil {
		trace.SpanFromContext(r.Context()).RecordError(err)
		httpStatusError(w, r, 404)
		return
	}

	w.Header().Add("Content-Type", getContentType(filePath))
	w.Header().Add("Content-Length", fmt.Sprintf("%d", len(content)))
	w.WriteHeader(200)
	if _, writeErr := w.Write(content); writeErr != nil {
		log.Printf("Error writing image response in DataImages handler: %v", writeErr)
	}
}

// WS : Web Sockets /ws/
func WS(w http.ResponseWriter, r *http.Request) {
	var err error
	// if r.Header.Get("Origin") != "http://" + r.Host {
	// 	httpStatusError(w, r, 403)
	// 	return
	// }

	u := websocket.Upgrader{ReadBufferSize: 1024, WriteBufferSize: 1024}

	conn, err := u.Upgrade(w, r, w.Header())

	if err != nil {
		ShowError(err, 0)
		http.Error(w, "Could not open websocket connection", http.StatusBadRequest)
		return
	}
	defer conn.Close()

	setGlobalDomain(r.Host)

	for {
		var request RequestStruct
		var response ResponseStruct
		response.Status = true

		select {
		case response.Alert = <-webAlerts:
		//
		default:
			//
		}

		err = conn.ReadJSON(&request)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("Error reading websocket message: %v", err)
			}
			break // Exit the loop on error
		}
		log.Printf("Received command: %s", request.Cmd)

		if !System.ConfigurationWizard {
			switch Settings.AuthenticationWEB {
			// Token Authentication
			case true:
				var token string
				tokens, ok := r.URL.Query()["Token"]

				if !ok || len(tokens[0]) < 1 {
					token = "-"
				} else {
					token = tokens[0]
				}

				newToken, err := tokenAuthentication(token)
				if err != nil {
					response.Status = false
					response.Reload = true
					response.Error = err.Error()
					request.Cmd = "-"

					if errWrite := conn.WriteJSON(&response); errWrite != nil {
						log.Printf("Error writing JSON response (token auth failed): %v", errWrite)
						break // Exit loop
					}
					continue // Continue to next message
				}
				response.Token = newToken
				response.Users, _ = authentication.GetAllUserData()
			}
		}

		switch request.Cmd {
		// Read Data
		case "getServerConfig":
			// response.Config = Settings
		case "updateLog":
			(&response).setDefaultResponseData(false)
			if errWrite := conn.WriteJSON(&response); errWrite != nil {
				log.Printf("Error writing JSON response (updateLog): %v", errWrite)
				break // Exit loop
			}
			continue
		case "loadFiles":
			// response.Response = Settings.Files

		// Save Data
		case "saveSettings":
			var authenticationUpdate = Settings.AuthenticationWEB
			var previousTLSMode = Settings.TLSMode
			var previousHostIP = Settings.HostIP
			var previousHostName = Settings.HostName
			var previousStoreBufferInRAM = Settings.StoreBufferInRAM
			var previousClearXMLTVCache = Settings.ClearXMLTVCache

			response.Settings, err = updateServerSettings(request)
			if err == nil {
				response.OpenMenu = strconv.Itoa(lo.IndexOf(System.WEB.Menu, "settings"))

				if Settings.AuthenticationWEB && !authenticationUpdate {
					response.Reload = true
				}

				if Settings.TLSMode != previousTLSMode {
					showInfo("Web server:" + "Toggling TLS mode")
					reinitialize()
					response.OpenLink = System.URLBase + "/web/"
					restartWebserver <- true
				}

				if Settings.HostIP != previousHostIP {
					showInfo("Web server:" + fmt.Sprintf("Changing host IP to %s", Settings.HostIP))
					reinitialize()
					response.OpenLink = System.URLBase + "/web/"
					restartWebserver <- true
				}

				if Settings.HostName != previousHostName {
					Settings.HostIP = previousHostName
					showInfo("Web server:" + fmt.Sprintf("Changing host name to %s", Settings.HostName))
					reinitialize()
					response.OpenLink = System.URLBase + "/web/"
					restartWebserver <- true
				}

				if Settings.StoreBufferInRAM != previousStoreBufferInRAM {
					initBufferVFS(Settings.StoreBufferInRAM)
				}

				if Settings.ClearXMLTVCache && !previousClearXMLTVCache {
					clearXMLTVCache()
				}
			}
		case "saveFilesM3U":
			err = saveFiles(request, "m3u")
			if err == nil {
				response.OpenMenu = strconv.Itoa(lo.IndexOf(System.WEB.Menu, "playlist"))
			}
		case "updateFileM3U":
			err = updateFile(request, "m3u")
			if err == nil {
				response.OpenMenu = strconv.Itoa(lo.IndexOf(System.WEB.Menu, "playlist"))
			}
		case "saveFilesHDHR":
			err = saveFiles(request, "hdhr")
			if err == nil {
				response.OpenMenu = strconv.Itoa(lo.IndexOf(System.WEB.Menu, "playlist"))
			}
		case "updateFileHDHR":
			err = updateFile(request, "hdhr")
			if err == nil {
				response.OpenMenu = strconv.Itoa(lo.IndexOf(System.WEB.Menu, "playlist"))
			}
		case "saveFilesXMLTV":
			err = saveFiles(request, "xmltv")
			if err == nil {
				response.OpenMenu = strconv.Itoa(lo.IndexOf(System.WEB.Menu, "xmltv"))
			}
		case "updateFileXMLTV":
			err = updateFile(request, "xmltv")
			if err == nil {
				response.OpenMenu = strconv.Itoa(lo.IndexOf(System.WEB.Menu, "xmltv"))
			}
		case "saveFilter":
			response.Settings, err = saveFilter(request)
			if err == nil {
				response.OpenMenu = strconv.Itoa(lo.IndexOf(System.WEB.Menu, "filter"))
			}
		case "saveEpgMapping":
			err = saveXEpgMapping(request)
		case "saveUserData":
			err = saveUserData(request)
			if err == nil {
				response.OpenMenu = strconv.Itoa(lo.IndexOf(System.WEB.Menu, "users"))
			}
		case "saveNewUser":
			err = saveNewUser(request)
			if err == nil {
				response.OpenMenu = strconv.Itoa(lo.IndexOf(System.WEB.Menu, "users"))
			}
		case "resetLogs":
			WebScreenLog.Mu.Lock()
			WebScreenLog.Log = make([]string, 0)
			WebScreenLog.Errors = 0
			WebScreenLog.Warnings = 0
			WebScreenLog.Mu.Unlock()
			response.OpenMenu = strconv.Itoa(lo.IndexOf(System.WEB.Menu, "log"))
		case "xteveBackup":
			file, errNew := xteveBackup()
			err = errNew
			if err == nil {
				response.OpenLink = fmt.Sprintf("%s://%s/download/%s", System.ServerProtocol.WEB, System.Domain, file)
			}
		case "xteveRestore":
			WebScreenLog.Mu.Lock()
			WebScreenLog.Log = make([]string, 0)
			WebScreenLog.Errors = 0
			WebScreenLog.Warnings = 0
			WebScreenLog.Mu.Unlock()

			if len(request.Base64) > 0 {
				newWebURL, err := xteveRestoreFromWeb(request.Base64)
				if err != nil {
					ShowError(err, 000)
					response.Alert = err.Error()
				}

				if err == nil {
					if len(newWebURL) > 0 {
						response.Alert = "Backup was successfully restored.\nThe port of the sTeVe URL has changed, you have to restart xTeVe.\nAfter a restart, xTeVe can be reached again at the following URL:\n" + newWebURL
					} else {
						response.Alert = "Backup was successfully restored."
						response.Reload = true
					}
					showInfo("xTeVe:" + "Backup successfully restored.")
				}
			}
		case "uploadLogo":
			if len(request.Base64) > 0 {
				response.LogoURL, err = uploadLogo(request.Base64, request.Filename)

				if err == nil {
					if errWrite := conn.WriteJSON(&response); errWrite != nil {
						log.Printf("Error writing JSON response (uploadLogo): %v", errWrite)
						break
					}
					continue
				}
				// If err from uploadLogo was not nil, it will be handled by the generic error handling below.
			}
		case "saveWizard":
			nextStep, errNew := saveWizard(request)

			err = errNew
			if err == nil {
				if nextStep == 10 {
					System.ConfigurationWizard = false
					response.Reload = true
				} else {
					response.Wizard = nextStep
				}
			}
		// case "wizardCompleted":
		// 	System.ConfigurationWizard = false
		// 	response.Reload = true
		default:
			fmt.Println("+ + + + + + + + + + +", request.Cmd)

			var requestMap = make(map[string]any) // Debug
			_ = requestMap
			if System.Dev {
				fmt.Println(mapToJSON(requestMap))
			}
		}

		if err != nil {
			response.Status = false
			response.Error = err.Error()
			response.Settings = Settings
		}

		(&response).setDefaultResponseData(true)
		if System.ConfigurationWizard {
			response.ConfigurationWizard = System.ConfigurationWizard
		}

		if errWrite := conn.WriteJSON(&response); errWrite != nil {
			log.Printf("Error writing main JSON response in WS handler: %v", errWrite)
			break
		}
	}
}

var webHandler http.Handler

func init() {
	htmlFS, err := fs.Sub(webUI, "html")
	if err != nil {
		log.Fatal("Failed to create sub-filesystem for embedded resources: ", err)
	}

	// Check if JS files exist.
	jsFiles, err := fs.ReadDir(htmlFS, "js")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			log.Fatal("Embedded 'html/js' directory not found. Please run 'make build' to compile the TypeScript files.")
		}
		log.Fatal("Failed to read embedded 'html/js' directory: ", err)
	}

	if len(jsFiles) == 0 {
		log.Fatal("No JavaScript files found in embedded 'html/js' directory. Please run 'make build' to compile the TypeScript files.")
	}

	fileServer := http.FileServer(http.FS(htmlFS))
	webHandler = http.StripPrefix("/web/", fileServer)
}

// Web : Web Server /web/
func Web(w http.ResponseWriter, r *http.Request) {
	var path = r.URL.Path

	// Serve static assets using the file server
	if !strings.HasSuffix(path, ".html") && !strings.HasSuffix(path, ".js") && path != "/web/" && path != "/web" {
		webHandler.ServeHTTP(w, r)
		return
	}

	var lang = make(map[string]any)
	var err error

	if path == "/web" {
		path = "/web/"
	}
	var requestFile = strings.Replace(path, "/web", "html", -1)
	var content string
	var contentBytes []byte
	var contentType, file string

	var language LanguageUI

	setGlobalDomain(r.Host)

	// Load language file
	var languageFile = fmt.Sprintf("html/lang/%s.json", Settings.Language)
	if System.Dev {
		lang, err = loadJSONFileToMap("src/" + languageFile)
		if err != nil {
			ShowError(err, 0)
		}
	} else {
		contentBytes, err = webUI.ReadFile(languageFile)
		if err != nil {
			// Fallback to English if language file is not found
			languageFile = "html/lang/en.json"
			contentBytes, err = webUI.ReadFile(languageFile)
			if err != nil {
				ShowError(err, 0)
				httpStatusError(w, r, 500)
				return
			}
		}
		err = json.Unmarshal(contentBytes, &lang)
		if err != nil {
			ShowError(err, 0)
		}
	}

	err = json.Unmarshal([]byte(mapToJSON(lang)), &language)
	if err != nil {
		ShowError(err, 0)
		return
	}

	if getFilenameFromPath(requestFile) == "html" {
		if System.ScanInProgress == 0 {
			if len(Settings.Files.M3U) == 0 && len(Settings.Files.HDHR) == 0 {
				System.ConfigurationWizard = true
			}
		}

		switch System.ConfigurationWizard {
		case true:
			file = requestFile + "configuration.html"
			Settings.AuthenticationWEB = false
		case false:
			file = requestFile + "index.html"
		}

		if System.ScanInProgress == 1 {
			file = requestFile + "maintenance.html"
		}

		switch Settings.AuthenticationWEB {
		case true:
			var username, password, confirm string
			switch r.Method {
			case "POST":
				var allUsers, _ = authentication.GetAllUserData()

				username = r.FormValue("username")
				password = r.FormValue("password")

				if len(allUsers) == 0 {
					confirm = r.FormValue("confirm")
				}

				// First user is created (Password confirmation is available)
				if len(confirm) > 0 {
					var token, err = createFirstUserForAuthentication(username, password)
					if err != nil {
						httpStatusError(w, r, 429)
						return
					}
					// Redirect so that the Data is deleted from the Browser.
					w = authentication.SetCookieToken(w, token)
					http.Redirect(w, r, "/web", http.StatusMovedPermanently)
					return
				}

				// Username and Password available, will now be checked
				if len(username) > 0 && len(password) > 0 {
					var token, err = authentication.UserAuthentication(username, password)
					if err != nil {
						file = requestFile + "login.html"
						lang["authenticationErr"] = language.Login.Failed
						break
					}
					w = authentication.SetCookieToken(w, token)
					http.Redirect(w, r, "/web", http.StatusMovedPermanently) // Redirect so that the Data is deleted from the Browser.
				} else {
					w = authentication.SetCookieToken(w, "-")
					http.Redirect(w, r, "/web", http.StatusMovedPermanently) // Redirect so that the Data is deleted from the Browser.
				}
				return
			case "GET":
				lang["authenticationErr"] = ""
				_, token, err := authentication.CheckTheValidityOfTheTokenFromHTTPHeader(w, r)

				if err != nil {
					file = requestFile + "login.html"
					break
				}

				err = checkAuthorizationLevel(token, "authentication.web")
				if err != nil {
					file = requestFile + "login.html"
					break
				}
			}

			allUserData, err := authentication.GetAllUserData()
			if err != nil {
				ShowError(err, 0)
				httpStatusError(w, r, 403)
				return
			}

			if len(allUserData) == 0 && Settings.AuthenticationWEB {
				file = requestFile + "create-first-user.html"
			}
		}
		requestFile = file
	}

	if System.Dev {
		contentBytes, err = os.ReadFile("src/" + requestFile)
		if err != nil {
			httpStatusError(w, r, 404)
			return
		}
	} else {
		contentBytes, err = webUI.ReadFile(requestFile)
		if err != nil {
			httpStatusError(w, r, 404)
			return
		}
	}

	contentType = getContentType(requestFile)
	w.Header().Add("Content-Type", contentType)

	if contentType == "text/html" || contentType == "application/javascript" {
		content = string(contentBytes)
		content = parseTemplate(content, lang)
		contentBytes = []byte(content)
	}

	w.WriteHeader(200)
	if _, writeErr := w.Write(contentBytes); writeErr != nil {
		log.Printf("Error writing response in Web handler: %v", writeErr)
	}
}

// API : API request /api/
func API(w http.ResponseWriter, r *http.Request) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		trace.SpanFromContext(r.Context()).RecordError(err)
		// Log the error for debugging, but still deny access.
		ShowError(fmt.Errorf("API: error parsing RemoteAddr '%s': %w", r.RemoteAddr, err), 0)
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	ip := net.ParseIP(host)
	if ip == nil {
		// Log the error for debugging, but still deny access.
		ShowError(fmt.Errorf("API: error parsing IP from host '%s' in RemoteAddr '%s'", host, r.RemoteAddr), 0)
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	if !ip.IsLoopback() {
		showWarning(2023)
		http.Error(w, "Forbidden - API access is restricted to localhost.", http.StatusForbidden)
		return
	}

	/*
			API conditions (without Authentication):
			- API must be activated in the Settings

			Example API Request with curl
			Status:
			curl -X POST -H "Content-Type: application/json" -d '{"cmd":"status"}' http://localhost:34400/api/

			- - - - -

			API conditions (with Authentication):
			- API must be activated in the Settings
			- API must be activated in the Authentication Settings
			- User must have API authorization

			A Token is generated after each API request, which is valid once every 60 minutes.
			A new Token is included in every answer

			Example API Request with curl
			Login request:
			curl -X POST -H "Content-Type: application/json" -d '{"cmd":"login","username":"plex","password":"123"}' http://localhost:34400/api/

			Response:
			{
		  	"status": true,
		  	"token": "U0T-NTSaigh-RlbkqERsHvUpgvaaY2dyRGuwIIvv"
			}

			Status Request using a Token:
			curl -X POST -H "Content-Type: application/json" -d '{"cmd":"status","token":"U0T-NTSaigh-RlbkqERsHvUpgvaaY2dyRGuwIIvv"}' http://localhost:4400/api/

			Response:
			{
			  "epg.source": "XEPG",
			  "status": true,
			  "streams.active": 7,
			  "streams.all": 63,
			  "streams.xepg": 2,
			  "token": "mXiG1NE1MrTXDtyh7PxRHK5z8iPI_LzxsQmY-LFn",
			  "url.dvr": "localhost:34400",
			  "url.m3u": "http://localhost:34400/m3u/xteve.m3u",
			  "url.xepg": "http://localhost:34400/xmltv/xteve.xml",
			  "version.api": "1.1.0",
			  "version.xteve": "1.3.0"
			}
	*/

	// Note: We intentionally avoid calling setGlobalDomain(r.Host) here to prevent overwriting
	// global state during concurrent requests or tests. setGlobalDomain sets System.Domain,
	// which is a global variable. In a real server this is refreshed on every request based on
	// the Host header, but in tests this can cause race conditions or incorrect values if
	// requests are made with different Host headers.
	// However, the original code called it. If we remove it, we must ensure System.Domain is correct.
	// For now, we'll leave it as is, but be aware of side effects in tests.
	setGlobalDomain(r.Host)
	var request APIRequestStruct
	var response APIResponseStruct

	var responseAPIError = func(err error) {
		var response APIResponseStruct

		response.Status = false
		response.Error = err.Error()
		if _, writeErr := w.Write([]byte(mapToJSON(response))); writeErr != nil {
			log.Printf("Error writing error JSON response in API handler: %v", writeErr)
		}
	}

	response.Status = true

	if r.Method == "GET" {
		httpStatusError(w, r, 404)
		return
	}

	b, err := io.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil {
		httpStatusError(w, r, 400)
		return
	}

	err = json.Unmarshal(b, &request)
	if err != nil {
		httpStatusError(w, r, 400)
		return
	}

	w.Header().Set("content-type", "application/json")

	if Settings.AuthenticationAPI {
		var token string
		switch len(request.Token) {
		case 0:
			if request.Cmd == "login" {
				token, err = authentication.UserAuthentication(request.Username, request.Password)
				if err != nil {
					responseAPIError(err)
					return
				}
			} else {
				err = errors.New("login incorrect")
				if err != nil {
					responseAPIError(err)
					return
				}
			}
		default:
			token, err = tokenAuthentication(request.Token)
			fmt.Println(err)
			if err != nil {
				responseAPIError(err)
				return
			}
		}
		err = checkAuthorizationLevel(token, "authentication.api")
		if err != nil {
			responseAPIError(err)
			return
		}
		response.Token = token
	}

	switch request.Cmd {
	case "login": // Nothing has to be handed over
	case "status":
		response.VersionXteve = System.Version
		response.VersionAPI = System.APIVersion
		response.StreamsActive = int64(len(Data.Streams.Active))
		response.StreamsAll = int64(len(Data.Streams.All))
		response.StreamsXepg = int64(Data.XEPG.XEPGCount)
		response.EpgSource = Settings.EpgSource
		response.URLDvr = System.Domain
		response.URLM3U = System.ServerProtocol.M3U + "://" + System.Domain + "/m3u/xteve.m3u"
		response.URLWebDAV = System.ServerProtocol.WEB + "://" + System.Domain + "/dav/"
		response.URLXepg = System.ServerProtocol.XML + "://" + System.Domain + "/xmltv/xteve.xml"
		response.OtelExporterType = os.Getenv("OTEL_EXPORTER_TYPE")
		response.OtelExporterEndpoint = os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")

		BufferInformation.Range(func(k, v any) bool {
			if playlist, ok := v.(*Playlist); ok {
				response.TunerActive += int64(len(playlist.Streams))
				response.TunerAll += int64(playlist.Tuner)
			}
			return true
		})
		log.Printf("API Status: Found %d active tuners.", response.TunerActive)
	case "update.m3u":
		err = getProviderData("m3u", "")
		if err != nil {
			break
		}
		err = buildDatabaseDVR()
		if err != nil {
			break
		}
	case "update.hdhr":
		err = getProviderData("hdhr", "")
		if err != nil {
			break
		}
		err = buildDatabaseDVR()
		if err != nil {
			break
		}
	case "update.xmltv":
		err = getProviderData("xmltv", "")
		if err != nil {
			break
		}
	case "update.xepg":
		err = buildXEPG(false)
	default:
		err = errors.New(getErrMsg(5000))
	}

	if err != nil {
		responseAPIError(err)
		return // Ensure we don't try to write a success response if there was an error
	}

	if _, writeErr := w.Write([]byte(mapToJSON(response))); writeErr != nil {
		log.Printf("Error writing main JSON response in API handler: %v", writeErr)
	}
}

// Download : File Download
func Download(w http.ResponseWriter, r *http.Request) {
	var path = r.URL.Path
	var file = System.Folder.Temp + getFilenameFromPath(path)
	w.Header().Set("Content-Disposition", "attachment; filename="+getFilenameFromPath(file))

	content, err := readStringFromFile(file)
	if err != nil {
		w.WriteHeader(404)
		return
	}

	if errRemove := os.RemoveAll(System.Folder.Temp + getFilenameFromPath(path)); errRemove != nil {
		log.Printf("Error removing temporary file %s after download: %v", file, errRemove)
		// Continue to send content even if removal failed.
	}
	if _, writeErr := w.Write([]byte(content)); writeErr != nil {
		log.Printf("Error writing download response: %v", writeErr)
	}
}

func (rs *ResponseStruct) setDefaultResponseData(data bool) {
	// Always transfer the following Data to the Client
	rs.ClientInfo.ARCH = System.ARCH
	rs.ClientInfo.EpgSource = Settings.EpgSource
	rs.ClientInfo.DVR = System.Addresses.DVR
	rs.ClientInfo.M3U = System.Addresses.M3U
	rs.ClientInfo.XML = System.Addresses.XML
	rs.ClientInfo.OS = System.OS
	rs.ClientInfo.Streams = fmt.Sprintf("%d / %d", len(Data.Streams.Active), len(Data.Streams.All))
	rs.ClientInfo.UUID = Settings.UUID
	WebScreenLog.Mu.RLock()
	rs.ClientInfo.Errors = WebScreenLog.Errors
	rs.ClientInfo.Warnings = WebScreenLog.Warnings
	WebScreenLog.Mu.RUnlock()
	rs.IPAddressesV4Host = System.IPAddressesV4Host
	rs.Settings.HostIP = Settings.HostIP
	rs.Notification = System.Notification
	rs.Log = &WebScreenLog
	rs.ClientInfo.Version = fmt.Sprintf("%s (%s)", System.Version, System.Build)

	if data {
		rs.Users, _ = authentication.GetAllUserData()
		//rs.DVR = System.DVRAddress
		if Settings.EpgSource == "XEPG" {
			rs.ClientInfo.XEPGCount = Data.XEPG.XEPGCount
			var XEPG = make(map[string]any)
			if len(Data.Streams.Active) > 0 {
				XEPG["epgMapping"] = Data.XEPG.Channels
				XEPG["xmltvMap"] = Data.XMLTV.Mapping
			} else {
				XEPG["epgMapping"] = make(map[string]any)
				XEPG["xmltvMap"] = make(map[string]any)
			}
			rs.XEPG = XEPG
		}
		rs.Settings = Settings
		rs.Data.Playlist.M3U.Groups.Text = Data.Playlist.M3U.Groups.Text
		rs.Data.Playlist.M3U.Groups.Value = Data.Playlist.M3U.Groups.Value
		rs.Data.StreamPreviewUI.Active = Data.StreamPreviewUI.Active
		rs.Data.StreamPreviewUI.Inactive = Data.StreamPreviewUI.Inactive
	}
}

// withRouteTag wraps a handler to manually add the http.route attribute to spans and metrics.
// This is necessary because otelhttp.WithRouteTag is deprecated, and automatic route detection
// works best when handlers are wrapped directly, not when using a global middleware over a mux.
func withRouteTag(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if route := r.Pattern; route != "" {
			if labeler, ok := otelhttp.LabelerFromContext(r.Context()); ok {
				labeler.Add(attribute.String("http.route", route))
			}
			trace.SpanFromContext(r.Context()).SetAttributes(attribute.String("http.route", route))
		}
		next.ServeHTTP(w, r)
	})
}

func newHTTPHandler() http.Handler {
	mux := http.NewServeMux()

	handleFunc := func(pattern string, handlerFunc func(http.ResponseWriter, *http.Request)) {
		mux.Handle(pattern, withRouteTag(http.HandlerFunc(handlerFunc)))
	}

	handleFunc("/", Index)
	handleFunc("/stream/", Stream)
	handleFunc("/xmltv/", xTeVe)
	handleFunc("/m3u/", xTeVe)
	handleFunc("/data/", WS)
	handleFunc("/web/", Web)
	handleFunc("/download/", Download)
	handleFunc("/api/", API)
	handleFunc("/images/", Images)
	handleFunc("/data_images/", DataImages)

	davHandler := &webdav.Handler{
		Prefix:     "/dav/",
		FileSystem: &WebDAVFS{},
		LockSystem: webdav.NewMemLS(),
		Logger: func(r *http.Request, err error) {
			if err != nil {
				log.Printf("WEBDAV ERROR: %s", err)
			}
		},
	}
	mux.Handle("/dav/", withRouteTag(davHandler))

	handler := otelhttp.NewHandler(mux, "/")
	return handler
}

func httpStatusError(w http.ResponseWriter, _ *http.Request, httpStatusCode int) {
	http.Error(w, fmt.Sprintf("%s [%d]", http.StatusText(httpStatusCode), httpStatusCode), httpStatusCode)
}

func getContentType(filename string) (contentType string) {
	if strings.HasSuffix(filename, ".html") {
		contentType = "text/html"
	} else if strings.HasSuffix(filename, ".css") {
		contentType = "text/css"
	} else if strings.HasSuffix(filename, ".js") {
		contentType = "application/javascript"
	} else if strings.HasSuffix(filename, ".png") {
		contentType = "image/png"
	} else if strings.HasSuffix(filename, ".jpg") {
		contentType = "image/jpeg"
	} else if strings.HasSuffix(filename, ".gif") {
		contentType = "image/gif"
	} else if strings.HasSuffix(filename, ".svg") {
		contentType = "image/svg+xml"
	} else if strings.HasSuffix(filename, ".mp4") {
		contentType = "video/mp4"
	} else if strings.HasSuffix(filename, ".webm") {
		contentType = "video/webm"
	} else if strings.HasSuffix(filename, ".ogg") {
		contentType = "video/ogg"
	} else if strings.HasSuffix(filename, ".mp3") {
		contentType = "audio/mp3"
	} else if strings.HasSuffix(filename, ".wav") {
		contentType = "audio/wav"
	} else if strings.HasSuffix(filename, ".ico") {
		contentType = "image/x-icon"
	} else if strings.HasSuffix(filename, ".json") {
		contentType = "application/json"
	} else {
		contentType = "text/plain"
	}
	return
}
