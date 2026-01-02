// Copyright 2019 marmei. All rights reserved.
// Copyright 2022 senexcrenshaw. All rights reserved.
// Use of this source code is governed by a MIT license that can be found in the
// LICENSE file.
// GitHub: https://github.com/SenexCrenshaw/xTeVe

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"

	"xteve/src"
	"xteve/src/snap"
	"xteve/src/tracing"
)

// Name : Program Name
const Name = "xTeVe"

// DBVersion : Database Version
const DBVersion = "2.3.0"

// APIVersion : API Version
const APIVersion = "1.1.0"

var homeDirectory = fmt.Sprintf("%s%s.%s%s", src.GetUserHomeDirectory(), string(os.PathSeparator), strings.ToLower(Name), string(os.PathSeparator))
var samplePath = fmt.Sprintf("%spath%sto%sxteve%s", string(os.PathSeparator), string(os.PathSeparator), string(os.PathSeparator), string(os.PathSeparator))
var sampleRestore = fmt.Sprintf("%spath%sto%sfile%s", string(os.PathSeparator), string(os.PathSeparator), string(os.PathSeparator), string(os.PathSeparator))

var configFolder = flag.String("config", "", ": Config Folder        ["+samplePath+"] (default: "+homeDirectory+")")
var port = flag.String("port", "", ": Server port          [34400] (default: 34400)")
var restore = flag.String("restore", "", ": Restore from backup  ["+sampleRestore+"xteve_backup.zip]")

var debug = flag.Int("debug", 0, ": Debug level          [0 - 3] (default: 0)")
var info = flag.Bool("info", false, ": Show system info")
var version = flag.Bool("version", false, ": Show system version")
var h = flag.Bool("h", false, ": Show help")

// Activates Development Mode. The local Files are then used for the Webserver.
var dev = flag.Bool("dev", false, ": Activates the developer mode, the source code must be available. The local files for the web interface are used.")

func main() {
	if err := run(); err != nil {
		log.Fatalln(err)
	}
}

func run() (err error) {
	// Handle SIGINT (CTRL+C) gracefully.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// Separate Build Number from Version Number
	var build = strings.Split(src.Version, ".")

	var system = &src.System
	system.APIVersion = APIVersion
	// system.Branch = GitHub.Branch // Removed as GitHub global is removed
	system.Build = build[len(build)-1:][0]
	system.DBVersion = DBVersion
	// system.GitHub = GitHub // Removed as GitHub global is removed
	system.Name = Name
	system.Version = strings.Join(build[0:len(build)-1], ".")

	flag.Parse()

	if *h {
		flag.Usage()
		return nil
	}

	if *version {
		src.ShowSystemVersion()
		return nil
	}

	system.Dev = *dev

	// Set up OpenTelemetry.
	if err := snap.LoadEnv("otel.env"); err != nil {
		log.Printf("could not load otel.env from snap: %v", err)
	}

	otelExporterType := os.Getenv("OTEL_EXPORTER_TYPE")
	if otelExporterType == "" {
		otelExporterType, err = snap.Get("otel-exporter-type")
		if err != nil {
			log.Printf("could not get otel-exporter-type from snap: %v", err)
		}
	}

	otelShutdown, err := tracing.SetupOTelSDK(ctx, tracing.ExporterType(otelExporterType))
	if err != nil {
		return
	}
	// Handle shutdown properly so nothing leaks.
	defer func() {
		err = errors.Join(err, otelShutdown(context.Background()))
	}()

	// Panic
	defer func() {

		if r := recover(); r != nil {

			fmt.Println()
			fmt.Println("* * * * * FATAL ERROR * * * * *")
			fmt.Println("OS:  ", runtime.GOOS)
			fmt.Println("Arch:", runtime.GOARCH)
			fmt.Println("Err: ", r)
			fmt.Println()

			pc := make([]uintptr, 20)
			runtime.Callers(2, pc)

			for i := range pc {

				if runtime.FuncForPC(pc[i]) != nil {

					f := runtime.FuncForPC(pc[i])
					file, line := f.FileLine(pc[i])

					if string(file)[0:1] != "?" {
						fmt.Printf("%s:%d %s\n", filepath.Base(file), line, f.Name())
					}

				}

			}

			fmt.Println()
			fmt.Println("* * * * * * * * * * * * * * * *")

		}

	}()

	// Display System Information
	// Display System Information
	if *info {
		system.Flag.Info = true

		err := src.Init()
		if err != nil {
			src.ShowError(err, 0)
			os.Exit(0)
		}

		src.ShowSystemInfo()
		return nil

	}

	// Webserver Port
	if len(*port) > 0 {
		system.Flag.Port = *port
	}

	// Debug Level
	system.Flag.Debug = *debug
	if system.Flag.Debug > 3 {
		flag.Usage()
		return nil
	}

	// Storage location for the Configuration Files
	if len(*configFolder) > 0 {
		system.Folder.Config = *configFolder
	}

	// Restore Backup
	if len(*restore) > 0 {

		system.Flag.Restore = *restore

		err := src.Init()
		if err != nil {
			src.ShowError(err, 0)
			os.Exit(0)
		}

		err = src.XteveRestoreFromCLI(*restore)
		if err != nil {
			src.ShowError(err, 0)
		}

		os.Exit(0)
	}

	err = src.Init()
	if err != nil {
		src.ShowError(err, 0)
		os.Exit(0)
	}

	err = src.StartSystem(false)
	if err != nil {
		src.ShowError(err, 0)
		os.Exit(0)
	}

	err = src.InitMaintenance()
	if err != nil {
		src.ShowError(err, 0)
		os.Exit(0)
	}

	go func() {
		if err := src.StartWebserver(); err != nil {
			src.ShowError(err, 0)
			os.Exit(0)
		}
	}()

	// Wait for interruption.
	<-ctx.Done()

	// Stop receiving signal notifications as soon as possible.
	stop()

	return nil
}
