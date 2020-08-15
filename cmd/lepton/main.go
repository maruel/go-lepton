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

	"github.com/maruel/go-lepton/leptontest"
	"github.com/maruel/interrupt"
	"periph.io/x/periph/conn/i2c/i2creg"
	"periph.io/x/periph/conn/spi/spireg"
	"periph.io/x/periph/devices/lepton"
	"periph.io/x/periph/devices/lepton/image14bit"
	"periph.io/x/periph/host"
)

func mainImpl() error {
	cpuprofile := flag.String("cpuprofile", "", "dump CPU profile in file")
	port := flag.Int("port", 8010, "http port to listen on")
	noPush := flag.Bool("nopush", false, "do not push to server even if configured")
	verbose := flag.Bool("verbose", false, "enable log output")
	fake := flag.Bool("fake", false, "use a fake camera mock, useful to test without the hardware")
	i2cName := flag.String("i2c", "", "I²C bus to use")
	spiName := flag.String("spi", "", "SPI bus to use")
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

	if _, err := host.Init(); err != nil {
		return err
	}

	var err error
	var dev leptontest.Lepton
	if !*fake {
		spiBus, err := spireg.Open(*spiName)
		if err != nil {
			return err
		}
		defer spiBus.Close()
		i2cBus, err := i2creg.Open(*i2cName)
		if err != nil {
			return err
		}
		defer i2cBus.Close()
		if dev, err = lepton.New(spiBus, i2cBus); err != nil {
			return fmt.Errorf("%s\nIf testing without hardware, use -fake to simulate a camera", err)
		}
	} else if dev, err = leptontest.New(); err != nil {
		return err
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
			b := image14bit.NewGray14(dev.Bounds())
			f := &lepton.Frame{Gray14: b}
			if err := dev.NextFrame(f); err != nil {
				log.Printf("%v", err)
			}
			c <- f
			if d != nil {
				d <- f
			}
		}
	}()

	//w := StartWebServer(dev, c, *port)
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
	return watchFile()
}

func main() {
	if err := mainImpl(); err != nil {
		fmt.Fprintf(os.Stderr, "\nlepton: %s.\n", err)
		os.Exit(1)
	}
}
