package src

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/koron/go-ssdp"
)

// SSDP : SSPD / DLNA Server
func SSDP() (err error) {
	if !Settings.SSDP || System.Flag.Info {
		return
	}

	showInfo(fmt.Sprintf("SSDP / DLNA:%t", Settings.SSDP))

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)

	ad, err := ssdp.Advertise(
		"upnp:rootdevice", // send as "ST"
		fmt.Sprintf("uuid:%s::upnp:rootdevice", System.DeviceID), // send as "USN"
		fmt.Sprintf("%s/device.xml", System.URLBase),             // send as "LOCATION"
		System.AppName, // send as "SERVER"
		1800)           // send as "maxAge" in "CACHE-CONTROL"

	if err != nil {
		return
	}

	// Debug SSDP
	if System.Flag.Debug == 3 {
		ssdp.Logger = log.New(os.Stderr, "[SSDP] ", log.LstdFlags)
	}

	go func(adv *ssdp.Advertiser) {
		aliveTick := time.NewTicker(300 * time.Second)
	loop:
		for {
			select {
			case <-aliveTick.C:
				err = adv.Alive()
				if err != nil {
					ShowError(err, 0) // Original error from Alive()
					if byeErr := adv.Bye(); byeErr != nil {
						log.Printf("Error sending SSDP Bye after Alive failure: %v", byeErr)
					}
					if closeErr := adv.Close(); closeErr != nil {
						log.Printf("Error closing SSDP after Alive failure: %v", closeErr)
					}
					break loop
				}
			case <-quit:
				if byeErr := adv.Bye(); byeErr != nil {
					log.Printf("Error sending SSDP Bye on quit: %v", byeErr)
				}
				if closeErr := adv.Close(); closeErr != nil {
					log.Printf("Error closing SSDP on quit: %v", closeErr)
				}
				os.Exit(0) // This will terminate the program, so further error handling is moot.
				break loop
			}
		}
	}(ad)
	return
}
