package src

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/koron/go-ssdp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
)

// SSDP : SSPD / DLNA Server
func SSDP() (err error) {
	if !Settings.SSDP || System.Flag.Info {
		return
	}

	showInfo(fmt.Sprintf("SSDP / DLNA:%t", Settings.SSDP))

	tracer := otel.Tracer("xteve/ssdp")
	_, span := tracer.Start(context.Background(), "SSDP Init")
	defer span.End()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)

	var advertisers []*ssdp.Advertiser

	// Advertise Streaming Service
	ad, err := ssdp.Advertise(
		"upnp:rootdevice", // send as "ST"
		fmt.Sprintf("uuid:%s::upnp:rootdevice", System.DeviceID), // send as "USN"
		fmt.Sprintf("%s/device.xml", System.URLBase),             // send as "LOCATION"
		System.AppName, // send as "SERVER"
		1800)           // send as "maxAge" in "CACHE-CONTROL"

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return
	}
	advertisers = append(advertisers, ad)

	// Advertise WebDAV Service
	adDav, err := ssdp.Advertise(
		"urn:schemas-upnp-org:service:WebDAV:1", // send as "ST"
		fmt.Sprintf("uuid:%s::urn:schemas-upnp-org:service:WebDAV:1", System.DeviceID), // send as "USN"
		fmt.Sprintf("%s/dav/", System.URLBase),             // send as "LOCATION"
		System.AppName, // send as "SERVER"
		1800)           // send as "maxAge" in "CACHE-CONTROL"

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		// Cleanup first ad
		ad.Close()
		return
	}
	advertisers = append(advertisers, adDav)

	// Debug SSDP
	if System.Flag.Debug == 3 {
		ssdp.Logger = log.New(os.Stderr, "[SSDP] ", log.LstdFlags)
	}

	go func(advs []*ssdp.Advertiser) {
		aliveTick := time.NewTicker(300 * time.Second)
	loop:
		for {
			select {
			case <-aliveTick.C:
				_, spanAlive := tracer.Start(context.Background(), "SSDP Alive")

				var aliveErr error
				for _, adv := range advs {
					if err := adv.Alive(); err != nil {
						aliveErr = err
						break
					}
				}

				if aliveErr != nil {
					spanAlive.RecordError(aliveErr)
					spanAlive.SetStatus(codes.Error, aliveErr.Error())
					ShowError(aliveErr, 0) // Original error from Alive()

					_, spanBye := tracer.Start(context.Background(), "SSDP Bye")
					for _, adv := range advs {
						if byeErr := adv.Bye(); byeErr != nil {
							spanBye.RecordError(byeErr)
							log.Printf("Error sending SSDP Bye after Alive failure: %v", byeErr)
						}
					}
					spanBye.End()

					_, spanClose := tracer.Start(context.Background(), "SSDP Close")
					for _, adv := range advs {
						if closeErr := adv.Close(); closeErr != nil {
							spanClose.RecordError(closeErr)
							log.Printf("Error closing SSDP after Alive failure: %v", closeErr)
						}
					}
					spanClose.End()

					spanAlive.End()
					break loop
				}
				spanAlive.End()
			case <-quit:
				_, spanBye := tracer.Start(context.Background(), "SSDP Bye")
				for _, adv := range advs {
					if byeErr := adv.Bye(); byeErr != nil {
						spanBye.RecordError(byeErr)
						spanBye.SetStatus(codes.Error, byeErr.Error())
						log.Printf("Error sending SSDP Bye on quit: %v", byeErr)
					}
				}
				spanBye.End()

				_, spanClose := tracer.Start(context.Background(), "SSDP Close")
				for _, adv := range advs {
					if closeErr := adv.Close(); closeErr != nil {
						spanClose.RecordError(closeErr)
						spanClose.SetStatus(codes.Error, closeErr.Error())
						log.Printf("Error closing SSDP on quit: %v", closeErr)
					}
				}
				spanClose.End()
				os.Exit(0) // This will terminate the program, so further error handling is moot.
				break loop
			}
		}
	}(advertisers)
	return
}
