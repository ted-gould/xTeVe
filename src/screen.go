package src

import (
	"fmt"
	"log"
	"runtime"
	"strings"
	"sync"
	"time"
)

func showInfo(str string) {
	if System.Flag.Info {
		return
	}

	var max = 23
	var msg = strings.SplitN(str, ":", 2)
	var length = len(msg[0])
	var space string

	if len(msg) == 2 {
		for i := length; i < max; i++ {
			space = space + " "
		}

		msg[0] = msg[0] + ":" + space

		var logMsg = fmt.Sprintf("[%s] %s%s", System.Name, msg[0], msg[1])

		printLogOnScreen(logMsg, "info")

		logMsg = strings.Replace(logMsg, " ", "&nbsp;", -1)
		WebScreenLog.Log = append(WebScreenLog.Log, time.Now().Format("2006-01-02 15:04:05")+" "+logMsg)
		logCleanUp()
	}
}

func showDebug(str string, level int) {
	if System.Flag.Debug < level {
		return
	}

	var max = 23
	var msg = strings.SplitN(str, ":", 2)
	var length = len(msg[0])
	var space string
	var mutex = sync.RWMutex{}

	if len(msg) == 2 {
		for i := length; i < max; i++ {
			space = space + " "
		}
		msg[0] = msg[0] + ":" + space

		var logMsg = fmt.Sprintf("[DEBUG] %s%s", msg[0], msg[1])

		printLogOnScreen(logMsg, "debug")

		mutex.Lock()
		logMsg = strings.Replace(logMsg, " ", "&nbsp;", -1)
		WebScreenLog.Log = append(WebScreenLog.Log, time.Now().Format("2006-01-02 15:04:05")+" "+logMsg)
		logCleanUp()
		mutex.Unlock()
	}
}

func showHighlight(str string) {
	var max = 23
	var msg = strings.SplitN(str, ":", 2)
	var length = len(msg[0])
	var space string

	var notification Notification
	notification.Type = "info"

	if len(msg) == 2 {
		for i := length; i < max; i++ {
			space = space + " "
		}

		msg[0] = msg[0] + ":" + space

		var logMsg = fmt.Sprintf("[%s] %s%s", System.Name, msg[0], msg[1])

		printLogOnScreen(logMsg, "highlight")
	}

	notification.Type = "info"
	notification.Message = msg[1]

	if err := addNotification(notification); err != nil {
		ShowError(err, 0)
	}
}

func showWarning(errCode int) {
	var errMsg = getErrMsg(errCode)
	var logMsg = fmt.Sprintf("[%s] [WARNING] %s", System.Name, errMsg)
	var mutex = sync.RWMutex{}

	printLogOnScreen(logMsg, "warning")

	mutex.Lock()
	WebScreenLog.Log = append(WebScreenLog.Log, time.Now().Format("2006-01-02 15:04:05")+" "+logMsg)
	WebScreenLog.Warnings++
	mutex.Unlock()
}

// ShowError : Shows the Error Messages in the Console
func ShowError(err error, errCode int) {
	var mutex = sync.RWMutex{}

	var errMsg = getErrMsg(errCode)
	var logMsg = fmt.Sprintf("[%s] [ERROR] %s (%s) - EC: %d", System.Name, err, errMsg, errCode)

	printLogOnScreen(logMsg, "error")

	mutex.Lock()
	WebScreenLog.Log = append(WebScreenLog.Log, time.Now().Format("2006-01-02 15:04:05")+" "+logMsg)
	WebScreenLog.Errors++
	mutex.Unlock()
}

func printLogOnScreen(logMsg string, logType string) {
	var color string

	switch logType {
	case "info":
		color = "\033[0m"
	case "debug":
		color = "\033[35m"
	case "highlight":
		color = "\033[32m"
	case "warning":
		color = "\033[33m"
	case "error":
		color = "\033[31m"
	}

	switch runtime.GOOS {
	case "windows":
		log.Println(logMsg)
	default:
		fmt.Print(color)
		log.Println(logMsg)
		fmt.Print("\033[0m")
	}
}

func logCleanUp() {
	var logEntriesRAM = Settings.LogEntriesRAM
	var logs = WebScreenLog.Log

	WebScreenLog.Warnings = 0
	WebScreenLog.Errors = 0

	if len(logs) > logEntriesRAM {
		var tmp = make([]string, 0)
		for i := len(logs) - logEntriesRAM; i < logEntriesRAM; i++ {
			tmp = append(tmp, logs[i])
		}
		logs = tmp
	}

	for _, log := range logs {
		if strings.Contains(log, "WARNING") {
			WebScreenLog.Warnings++
		}

		if strings.Contains(log, "ERROR") {
			WebScreenLog.Errors++
		}
	}
	WebScreenLog.Log = logs
}
