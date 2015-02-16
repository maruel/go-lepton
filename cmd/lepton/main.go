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
)

func printStats(l *lepton.Lepton, s *Seeder) {
	started := time.Now()
	for !interrupt.IsSet() {
		leptonStats := l.Stats()
		var seederStats SeederStats
		if s != nil {
			seederStats = s.Stats()
		}
		duration := time.Now().Sub(started)
		fmt.Printf(
			"\r%d frames %d duped %d dummy %d badsync %d broken %d fail %d HTTP %d Imgs %.1fs",
			leptonStats.GoodFrames, leptonStats.DuplicateFrames,
			leptonStats.DummyLines, leptonStats.SyncFailures,
			leptonStats.BrokenPackets, leptonStats.TransferFails,
			seederStats.HTTPReqs, seederStats.ImgsSent,
			duration.Seconds())
		time.Sleep(time.Second)
	}
	fmt.Print("\n")
}

func mainImpl() error {
	cpuprofile := flag.String("cpuprofile", "", "dump CPU profile in file")
	port := flag.Int("port", 8010, "http port to listen on")
	noPush := flag.Bool("nopush", false, "do not push to server even if configured")
	verbose := flag.Bool("verbose", false, "enable log output")
	query := flag.Bool("query", false, "query the camera then quit")
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

	l, err := lepton.MakeLepton("", 0)
	if l != nil {
		defer l.Close()
	}
	if err != nil {
		return err
	}

	if serial, err := l.GetSerial(); err == nil {
		fmt.Printf("Lepton serial: 0x%x\n", serial)
	}
	if uptime, err := l.GetUptime(); err == nil {
		fmt.Printf("Lepton uptime: %.2fs\n", uptime.Seconds())
	}
	if temp, err := l.GetTemperature(); err == nil {
		fmt.Printf("Lepton temp: %.2fK\n", float32(temp)*0.001)
	}
	if temp, err := l.GetTemperatureHousing(); err == nil {
		fmt.Printf("Lepton temp: %.2fK (housing)\n", float32(temp)*0.001)
	}
	if *query {
		return nil
	}

	var s *Seeder
	if !*noPush {
		s = LoadSeeder()
	}

	c := make(chan *lepton.LeptonBuffer, 9*60)
	var d chan *lepton.LeptonBuffer
	if s != nil {
		d = make(chan *lepton.LeptonBuffer, 9*60)
	}

	// Lepton reader loop.
	go func() {
		for {
			// Keep this loop busy to not lose sync on SPI.
			b := &lepton.LeptonBuffer{}
			l.ReadImg(b)
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
			w.SetImg(<-c)
		}
	}()

	if d != nil {
		go s.sendImages(d)
	}

	fmt.Printf("\n")
	printStats(l, s)
	return nil
}

func main() {
	if err := mainImpl(); err != nil {
		fmt.Fprintf(os.Stderr, "\ngo-lepton: %s.\n", err)
		os.Exit(1)
	}
}
