// Copyright 2015 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"runtime/pprof"
	"time"

	"github.com/maruel/go-lepton/lepton"
	"github.com/maruel/interrupt"
	//"github.com/maruel/subcommands"
)

/*
var application = &subcommands.DefaultApplication{
	Name:  "lepton",
	Title: "Lepton drives a FLIR Lepton on a Raspberry Pi.",
	Commands: []*subcommands.Command{
		subcommands.CmdHelp,
		cmdPowerOff,
		cmdPowerOn,
		cmdQuery,
		cmdRun,
	},
}
*/

func printStats(l lepton.Lepton, s *Seeder, noCR bool) {
	started := time.Now()
	format := "\rframes: %d good %d duped; lines: %d good %d discard %d badsync %d broken; %d fail %d resets; %d HTTP %d Imgs %.1fs"
	if noCR {
		format = format[1:] + "\n"
	}
	for !interrupt.IsSet() {
		leptonStats := l.Stats()
		var seederStats SeederStats
		if s != nil {
			seederStats = s.Stats()
		}
		duration := time.Now().Sub(started)
		fmt.Printf(
			format,
			leptonStats.GoodFrames, leptonStats.DuplicateFrames,
			leptonStats.GoodLines, leptonStats.DiscardLines, leptonStats.BadSyncLines,
			leptonStats.BrokenLines, leptonStats.TransferFails, leptonStats.Resets,
			seederStats.HTTPReqs, seederStats.ImgsSent,
			duration.Seconds())
		if noCR {
			time.Sleep(2 * time.Second)
		} else {
			time.Sleep(time.Second)
		}
	}
	fmt.Print("\n")
}

/*
func main() {
	os.Exit(subcommands.Run(application, nil))
}
*/

func mainImpl() error {
	cpuprofile := flag.String("cpuprofile", "", "dump CPU profile in file")
	port := flag.Int("port", 8010, "http port to listen on")
	noPush := flag.Bool("nopush", false, "do not push to server even if configured")
	verbose := flag.Bool("verbose", false, "enable log output")
	query := flag.Bool("query", false, "query the camera then quit")
	fake := flag.Bool("fake", false, "use a fake camera mock, useful to test without the hardware")
	flag.Parse()

	if len(flag.Args()) != 0 {
		return fmt.Errorf("unexpected argument: %s", flag.Args())
	}

	if !*verbose {
		log.SetOutput(ioutil.Discard)
	}

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			return err
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	interrupt.HandleCtrlC()

	var err error
	var l lepton.Lepton
	if !*fake {
		l, err = lepton.MakeLepton("", 0)
		if err != nil {
			err = fmt.Errorf("%s\nIf testing without hardware, use -fake to simulate a camera", err)
		}
	} else {
		l, err = lepton.MakeFakeLepton("", 0)
	}
	if l != nil {
		defer l.Close()
	}
	if err != nil {
		return err
	}

	if status, err := l.GetStatus(); err == nil {
		fmt.Printf("Status.CameraStatus: %s\n", status.CameraStatus)
		fmt.Printf("Status.CommandCount: %d\n", status.CommandCount)
	}
	if serial, err := l.GetSerial(); err == nil {
		fmt.Printf("Serial:              0x%x\n", serial)
	}
	if uptime, err := l.GetUptime(); err == nil {
		fmt.Printf("Uptime:              %s\n", uptime)
	}
	if temp, err := l.GetTemperature(); err == nil {
		fmt.Printf("Temperature:         %s\n", temp)
	}
	if temp, err := l.GetTemperatureHousing(); err == nil {
		fmt.Printf("Temperature housing: %s\n", temp)
	}
	if tel, err := l.GetTelemetryEnable(); err == nil {
		fmt.Printf("Telemetry:           %s\n", tel)
	}
	if loc, err := l.GetTelemetryLocation(); err == nil {
		fmt.Printf("TelemetryLocation:   %s\n", loc)
	}
	if pos, err := l.GetShutterPosition(); err == nil {
		fmt.Printf("ShutterPosition:     %s\n", pos)
	}
	if mode, err := l.GetFFCModeControl(); err == nil {
		fmt.Printf("FCCMode.FFCShutterMode:          %s\n", mode.FFCShutterMode)
		fmt.Printf("FCCMode.ShutterTempLockoutState: %s\n", mode.ShutterTempLockoutState)
		fmt.Printf("FCCMode.VideoFreezeDuringFFC:    %s\n", mode.VideoFreezeDuringFFC)
		fmt.Printf("FCCMode.FFCDesired:              %s\n", mode.FFCDesired)
		fmt.Printf("FCCMode.ElapsedTimeSinceLastFFC: %s\n", mode.ElapsedTimeSinceLastFFC.ToDuration())
		fmt.Printf("FCCMode.DesiredFFCPeriod:        %s\n", mode.DesiredFFCPeriod.ToDuration())
		fmt.Printf("FCCMode.ExplicitCommandToOpen:   %s\n", mode.ExplicitCommandToOpen)
		fmt.Printf("FCCMode.DesiredFFCTempDelta:     %s\n", mode.DesiredFFCTempDelta)
		fmt.Printf("FCCMode.ImminentDelay:           %d\n", mode.ImminentDelay)
	}
	if *query {
		return nil
	}

	var s *Seeder
	if !*noPush {
		s = LoadSeeder()
	}

	c := make(chan *lepton.Frame, 9*60)
	var d chan *lepton.Frame
	if s != nil {
		d = make(chan *lepton.Frame, 9*60)
	}

	// Lepton reader loop.
	go func() {
		for {
			// Keep this loop busy to not lose sync on SPI.
			b := l.ReadImg()
			c <- b
			if d != nil {
				d <- b
			}
		}
	}()

	//w := StartWebServer(l, c, *port)
	w := StartWebServer(*port)
	go func() {
		for {
			w.AddImg(<-c)
		}
	}()

	if d != nil {
		go s.sendImages(d)
	}

	fmt.Printf("\n")
	printStats(l, s, *verbose)
	return nil
}

func main() {
	if err := mainImpl(); err != nil {
		fmt.Fprintf(os.Stderr, "\ngo-lepton: %s.\n", err)
		os.Exit(1)
	}
}
