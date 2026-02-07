package src

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log" // Added for log.Printf
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"xteve/src/internal/authentication"

	"github.com/gorilla/websocket"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/net/webdav"
)

// webAlerts channel to send to client
var webAlerts = make(chan string, 3)
var restartWebserver = make(chan bool, 1)

// Active HTTP connections counter
var activeHTTPConnections int64

func connState(c net.Conn, state http.ConnState) {
	switch state {
	case http.StateNew:
		atomic.AddInt64(&activeHTTPConnections, 1)
	case http.StateClosed:
		atomic.AddInt64(&activeHTTPConnections, -1)
	}
}

func init() {
	// Register types to ensure consistent behavior across platforms and replace custom logic
	types := map[string]string{
		".html": "text/html; charset=utf-8",
		".css":  "text/css; charset=utf-8",
		".js":   "application/javascript",
		".json": "application/json",
		".png":  "image/png",
		".jpg":  "image/jpeg",
		".jpeg": "image/jpeg",
		".gif":  "image/gif",
		".svg":  "image/svg+xml",
		".mp4":  "video/mp4",
		".webm": "video/webm",
		".ogg":  "video/ogg",
		".mp3":  "audio/mpeg",
		".wav":  "audio/wav",
		".ico":  "image/x-icon",
	}
	for ext, typ := range types {
		if err := mime.AddExtensionType(ext, typ); err != nil {
			panic(fmt.Sprintf("failed to register mime type %s: %v", ext, err))
		}
	}
}

// StartWebserver : Start the Webserver
func StartWebserver(startupSpan trace.Span) (err error) {
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
		server := http.Server{
			Addr:      ":" + port,
			Handler:   newHTTPHandler(),
			ConnState: connState,
		}

		currentSpan := startupSpan
		startupSpan = nil

		go func() {
			var err error

			if Settings.TLSMode {
				if !allFilesExist(System.File.ServerCertPrivKey, System.File.ServerCert) {
					if err = genCertFiles(); err != nil {
						ShowError(err, 7000)
					}
				}

				if currentSpan != nil {
					currentSpan.End()
				}

				err = server.ListenAndServeTLS(System.File.ServerCert, System.File.ServerCertPrivKey)
				if err != nil && err != http.ErrServerClosed {
					ShowError(err, 1017)
					err = server.ListenAndServe()
				}
			} else {
				if currentSpan != nil {
					currentSpan.End()
				}

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

// StartLocalSocketServer : Start a local Unix socket server for local tools like xteve-status
func StartLocalSocketServer() error {
	// Remove any existing socket file
	if err := os.RemoveAll(System.File.UnixSocket); err != nil {
		return fmt.Errorf("failed to remove existing socket: %w", err)
	}

	// Create Unix socket listener
	listener, err := net.Listen("unix", System.File.UnixSocket)
	if err != nil {
		return fmt.Errorf("failed to create Unix socket listener: %w", err)
	}

	// Set socket permissions to be accessible by the user only
	if err := os.Chmod(System.File.UnixSocket, 0600); err != nil {
		listener.Close()
		return fmt.Errorf("failed to set socket permissions: %w", err)
	}

	showInfo(fmt.Sprintf("Local Socket Server:%s", System.File.UnixSocket))

	// Create a simple mux for the local socket server
	mux := http.NewServeMux()
	mux.HandleFunc("/api/", API)

	server := &http.Server{
		Handler: mux,
	}

	// Start the server in a goroutine
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			ShowError(fmt.Errorf("local socket server error: %w", err), 0)
		}
	}()

	return nil
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
	var requestType, groupTitle, file, contentType string
	var err error
	var path = strings.TrimPrefix(r.URL.Path, "/")
	var groups = []string{}

	setGlobalDomain(r.Host)

	if strings.Contains(path, "xmltv/") {
		requestType = "xml"
	} else if strings.Contains(path, "m3u/") {
		requestType = "m3u"
	}

	// Check Authentication
	err = urlAuth(r, requestType)
	if err != nil {
		ShowError(err, 000)
		httpStatusError(w, r, 403)
		return
	}

	// XMLTV File
	if requestType == "xml" {
		_, childSpan := otel.Tracer("webserver").Start(r.Context(), "xmltv")
		defer childSpan.End()

		file = System.Folder.Data + filepath.Base(path)
		platformFile := getPlatformFile(file)

		if err := checkFile(platformFile); err != nil {
			childSpan.RecordError(err)
			httpStatusError(w, r, 404)
			return
		}

		f, err := os.Open(platformFile)
		if err != nil {
			childSpan.RecordError(err)
			httpStatusError(w, r, 404)
			return
		}
		defer f.Close()

		// Peek at first 512 bytes for content type detection
		buffer := make([]byte, 512)
		n, err := f.Read(buffer)
		if err != nil && err != io.EOF {
			childSpan.RecordError(err)
			httpStatusError(w, r, 500)
			return
		}

		contentType = http.DetectContentType(buffer[:n])
		if strings.Contains(strings.ToLower(contentType), "xml") {
			contentType = "application/xml; charset=utf-8"
		}

		// Reset file pointer to beginning
		if _, err := f.Seek(0, 0); err != nil {
			childSpan.RecordError(err)
			httpStatusError(w, r, 500)
			return
		}

		w.Header().Set("Content-Type", contentType)
		if _, writeErr := io.Copy(w, f); writeErr != nil {
			log.Printf("Error streaming response in xTeVe handler: %v", writeErr)
		}
		return
	}

	// M3U File
	if requestType == "m3u" {
		_, childSpan := otel.Tracer("webserver").Start(r.Context(), "m3u")
		defer childSpan.End()

		groupTitle = r.URL.Query().Get("group-title")

		if !System.Dev {
			// false: File name is set in the header
			// true: M3U is displayed directly in the browser
			w.Header().Set("Content-Disposition", "attachment; filename="+filepath.Base(path))
		}

		if len(groupTitle) > 0 {
			groups = strings.Split(groupTitle, ",")
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")

		bw := bufio.NewWriter(w)
		err = buildM3UToWriter(bw, groups)
		if err != nil {
			childSpan.RecordError(err)
			ShowError(err, 000)
		}
		if flushErr := bw.Flush(); flushErr != nil {
			// Only log flush error if we haven't already logged a build error
			if err == nil {
				childSpan.RecordError(flushErr)
				log.Printf("Error flushing M3U response: %v", flushErr)
			}
		}
	}
}

// Images : Image Cache /images/
func Images(w http.ResponseWriter, r *http.Request) {
	var path = strings.TrimPrefix(r.URL.Path, "/")
	var filePath = System.Folder.ImagesCache + filepath.Base(path)

	content, err := readByteFromFile(filePath)
	if err != nil {
		trace.SpanFromContext(r.Context()).RecordError(err)
		httpStatusError(w, r, 404)
		return
	}

	// Security: Prevent Stored XSS via SVG files by enforcing strict CSP (sandbox)
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; img-src 'self'; style-src 'unsafe-inline';")
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
	var filePath = System.Folder.ImagesUpload + filepath.Base(path)

	content, err := readByteFromFile(filePath)
	if err != nil {
		trace.SpanFromContext(r.Context()).RecordError(err)
		httpStatusError(w, r, 404)
		return
	}

	// Security: Prevent Stored XSS via SVG files by enforcing strict CSP (sandbox)
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; img-src 'self'; style-src 'unsafe-inline';")
	w.Header().Add("Content-Type", getContentType(filePath))
	w.Header().Add("Content-Length", fmt.Sprintf("%d", len(content)))
	w.WriteHeader(200)
	if _, writeErr := w.Write(content); writeErr != nil {
		log.Printf("Error writing image response in DataImages handler: %v", writeErr)
	}
}

// Rate Limiter for Login
var loginRateLimiter = struct {
	sync.Mutex
	attempts    map[string]int
	windowStart map[string]time.Time
}{
	attempts:    make(map[string]int),
	windowStart: make(map[string]time.Time),
}

// checkLoginRateLimit implements a Fixed Window rate limiter.
// Note: It relies on IP address. If xTeVe is behind a reverse proxy, all users might appear
// as the same IP (the proxy) unless the proxy transparency is handled elsewhere (not currently in xTeVe).
func checkLoginRateLimit(ip string) bool {
	return checkLoginRateLimitWithTime(ip, time.Now())
}

func checkLoginRateLimitWithTime(ip string, now time.Time) bool {
	loginRateLimiter.Lock()
	defer loginRateLimiter.Unlock()

	// Prevent memory leak: if map gets too big, clear old entries
	if len(loginRateLimiter.attempts) > 1000 {
		for k, start := range loginRateLimiter.windowStart {
			if now.Sub(start) > 10*time.Minute {
				delete(loginRateLimiter.attempts, k)
				delete(loginRateLimiter.windowStart, k)
			}
		}
		// Hard limit fallback
		if len(loginRateLimiter.attempts) > 2000 {
			loginRateLimiter.attempts = make(map[string]int)
			loginRateLimiter.windowStart = make(map[string]time.Time)
		}
	}

	// Fixed Window: reset count if current window expired (> 5 mins)
	if now.Sub(loginRateLimiter.windowStart[ip]) > 5*time.Minute {
		loginRateLimiter.attempts[ip] = 0
		loginRateLimiter.windowStart[ip] = now
	}

	loginRateLimiter.attempts[ip]++

	return loginRateLimiter.attempts[ip] <= 10
}

// isPrivateIP checks if an IP address is private or loopback
func isPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() {
		return true
	}
	return false
}

func getClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}

	// Parse IP
	remoteIP := net.ParseIP(host)
	if remoteIP == nil {
		return host // Fallback
	}

	// Check if RemoteAddr is private
	if isPrivateIP(remoteIP) {
		// Check X-Real-IP first (safer, as it's usually set by the immediate proxy)
		if xrip := r.Header.Get("X-Real-IP"); xrip != "" {
			return strings.TrimSpace(xrip)
		}

		// Check X-Forwarded-For
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			// X-Forwarded-For: client, proxy1, proxy2
			// The last IP in the list is the one that connected to the last trusted proxy.
			parts := strings.Split(xff, ",")
			if len(parts) > 0 {
				clientIP := strings.TrimSpace(parts[len(parts)-1])
				if clientIP != "" {
					return clientIP
				}
			}
		}
	}

	return host
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
	// The connection has been hijacked. ConnState will receive StateHijacked and will NOT receive StateClosed.
	// We must manually decrement the counter when this handler exits.
	defer atomic.AddInt64(&activeHTTPConnections, -1)
	defer conn.Close()

	// Security: Limit WebSocket message size to 32MB to prevent DoS (Unrestricted Resource Consumption)
	conn.SetReadLimit(33554432)

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
				response.OpenMenu = strconv.Itoa(slices.Index(System.WEB.Menu, "settings"))

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
				response.OpenMenu = strconv.Itoa(slices.Index(System.WEB.Menu, "playlist"))
			}
		case "updateFileM3U":
			err = updateFile(request, "m3u")
			if err == nil {
				response.OpenMenu = strconv.Itoa(slices.Index(System.WEB.Menu, "playlist"))
			}
		case "saveFilesHDHR":
			err = saveFiles(request, "hdhr")
			if err == nil {
				response.OpenMenu = strconv.Itoa(slices.Index(System.WEB.Menu, "playlist"))
			}
		case "updateFileHDHR":
			err = updateFile(request, "hdhr")
			if err == nil {
				response.OpenMenu = strconv.Itoa(slices.Index(System.WEB.Menu, "playlist"))
			}
		case "saveFilesXMLTV":
			err = saveFiles(request, "xmltv")
			if err == nil {
				response.OpenMenu = strconv.Itoa(slices.Index(System.WEB.Menu, "xmltv"))
			}
		case "updateFileXMLTV":
			err = updateFile(request, "xmltv")
			if err == nil {
				response.OpenMenu = strconv.Itoa(slices.Index(System.WEB.Menu, "xmltv"))
			}
		case "saveFilter":
			response.Settings, err = saveFilter(request)
			if err == nil {
				response.OpenMenu = strconv.Itoa(slices.Index(System.WEB.Menu, "filter"))
			}
		case "saveEpgMapping":
			err = saveXEpgMapping(request)
		case "saveUserData":
			err = saveUserData(request)
			if err == nil {
				response.OpenMenu = strconv.Itoa(slices.Index(System.WEB.Menu, "users"))
			}
		case "saveNewUser":
			err = saveNewUser(request)
			if err == nil {
				response.OpenMenu = strconv.Itoa(slices.Index(System.WEB.Menu, "users"))
			}
		case "resetLogs":
			WebScreenLog.Mu.Lock()
			WebScreenLog.Log = make([]string, 0)
			WebScreenLog.Errors = 0
			WebScreenLog.Warnings = 0
			WebScreenLog.Mu.Unlock()
			response.OpenMenu = strconv.Itoa(slices.Index(System.WEB.Menu, "log"))
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

	err = bindToStruct(lang, &language)
	if err != nil {
		ShowError(err, 0)
		return
	}

	if filepath.Base(requestFile) == "html" {
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
				if !checkLoginRateLimit(getClientIP(r)) {
					httpStatusError(w, r, http.StatusTooManyRequests)
					return
				}

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
					w = authentication.SetCookieToken(w, token, Settings.TLSMode)
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
					w = authentication.SetCookieToken(w, token, Settings.TLSMode)
					http.Redirect(w, r, "/web", http.StatusMovedPermanently) // Redirect so that the Data is deleted from the Browser.
				} else {
					w = authentication.SetCookieToken(w, "-", Settings.TLSMode)
					http.Redirect(w, r, "/web", http.StatusMovedPermanently) // Redirect so that the Data is deleted from the Browser.
				}
				return
			case "GET":
				lang["authenticationErr"] = ""
				_, token, err := authentication.CheckTheValidityOfTheTokenFromHTTPHeader(w, r, Settings.TLSMode)

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

	mediaType, _, _ := mime.ParseMediaType(contentType)
	if mediaType == "text/html" || mediaType == "application/javascript" {
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
	// Allow Unix socket connections (RemoteAddr will be "@" or similar for Unix sockets)
	// For Unix sockets, the network is "unix" and RemoteAddr doesn't contain an IP
	isUnixSocket := strings.HasPrefix(r.RemoteAddr, "@") || r.RemoteAddr == ""

	if !isUnixSocket {
		// For TCP connections, enforce loopback restriction
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

	// Security: Enforce Content-Type to prevent CSRF via simple requests (e.g. text/plain)
	// We check Contains because header might include charset (e.g. "application/json; charset=utf-8")
	if !strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		httpStatusError(w, r, http.StatusUnsupportedMediaType)
		return
	}

	// Limit request body to 1MB to prevent DoS (Unrestricted Resource Consumption)
	// The APIRequestStruct is small, so 1MB is more than enough.
	r.Body = http.MaxBytesReader(w, r.Body, 1048576)

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

	// Security: If Web Auth is enabled, we MUST also enforce API Auth to prevent
	// bypass via reverse proxies (where RemoteAddr is localhost).
	if Settings.AuthenticationAPI || Settings.AuthenticationWEB {
		var token string
		switch len(request.Token) {
		case 0:
			if request.Cmd == "login" {
				if !checkLoginRateLimit(getClientIP(r)) {
					responseAPIError(errors.New("too many login attempts"))
					return
				}
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
		response.ActiveHTTPConnections = atomic.LoadInt64(&activeHTTPConnections)
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

		Lock.RLock()
		BufferInformation.Range(func(k, v any) bool {
			if playlist, ok := v.(*Playlist); ok {
				response.TunerActive += int64(len(playlist.Streams))
				response.TunerAll += int64(playlist.Tuner)
			}
			return true
		})
		Lock.RUnlock()
		log.Printf("API Status: Found %d active tuners.", response.TunerActive)
	case "update.m3u":
		err = getProviderData(context.WithoutCancel(r.Context()), "m3u", "")
		if err != nil {
			break
		}
		err = buildDatabaseDVR()
		if err != nil {
			break
		}
	case "update.hdhr":
		err = getProviderData(context.WithoutCancel(r.Context()), "hdhr", "")
		if err != nil {
			break
		}
		err = buildDatabaseDVR()
		if err != nil {
			break
		}
	case "update.xmltv":
		err = getProviderData(context.WithoutCancel(r.Context()), "xmltv", "")
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
	// Security: Enforce authentication for downloads if Web Auth is enabled
	if Settings.AuthenticationWEB {
		var token string
		var cookie *http.Cookie
		var err error

		// 1. Try Cookie
		cookie, err = r.Cookie("Token")
		if err == nil {
			token = cookie.Value
		}

		// 2. Try Query Param (fallback)
		if len(token) == 0 {
			token = r.URL.Query().Get("token")
		}

		if len(token) == 0 {
			httpStatusError(w, r, http.StatusUnauthorized)
			return
		}

		// 3. Validate Token
		newToken, err := authentication.CheckTheValidityOfTheToken(token)
		if err != nil {
			httpStatusError(w, r, http.StatusUnauthorized)
			return
		}

		// 4. Check Authorization Level
		// Since Download is part of the Web UI features (backup), we check authentication.web
		err = checkAuthorizationLevel(newToken, "authentication.web")
		if err != nil {
			httpStatusError(w, r, http.StatusForbidden)
			return
		}

		// 5. Rotate Token (Set Cookie)
		authentication.SetCookieToken(w, newToken, Settings.TLSMode)
	}

	var path = r.URL.Path
	var file = System.Folder.Temp + filepath.Base(path)
	platformFile := getPlatformFile(file)

	if err := checkFile(platformFile); err != nil {
		w.WriteHeader(404)
		return
	}

	f, err := os.Open(platformFile)
	if err != nil {
		w.WriteHeader(404)
		return
	}
	defer func() {
		f.Close()
		if errRemove := os.RemoveAll(platformFile); errRemove != nil {
			log.Printf("Error removing temporary file %s after download: %v", platformFile, errRemove)
		}
	}()

	w.Header().Set("Content-Disposition", "attachment; filename="+filepath.Base(file))

	if _, writeErr := io.Copy(w, f); writeErr != nil {
		log.Printf("Error streaming download response: %v", writeErr)
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

func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src * data:; font-src 'self' data:; connect-src 'self'; media-src *; object-src 'none';")

		if Settings.TLSMode {
			w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		}

		next.ServeHTTP(w, r)
	})
}

func panicMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				span := trace.SpanFromContext(r.Context())

				var panicErr error
				switch x := err.(type) {
				case string:
					panicErr = errors.New(x)
				case error:
					panicErr = x
				default:
					panicErr = fmt.Errorf("panic: %v", x)
				}

				span.RecordError(panicErr)
				span.SetStatus(codes.Error, panicErr.Error())

				panic(err)
			}
		}()
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
				span := trace.SpanFromContext(r.Context())
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			}
		},
	}
	mux.Handle("/dav/", withRouteTag(davHandler))

	handler := panicMiddleware(mux)
	handler = securityHeadersMiddleware(handler)
	handler = otelhttp.NewHandler(handler, "/")
	return handler
}

func httpStatusError(w http.ResponseWriter, _ *http.Request, httpStatusCode int) {
	http.Error(w, fmt.Sprintf("%s [%d]", http.StatusText(httpStatusCode), httpStatusCode), httpStatusCode)
}

func getContentType(filename string) string {
	return cmp.Or(mime.TypeByExtension(filepath.Ext(filename)), "text/plain")
}
