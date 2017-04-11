// Copyright 2017 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

// lepton-grab captures a single image.
package main

import (
	"errors"
	"flag"
	"fmt"
	"image"
	"image/png"
	"io/ioutil"
	"log"
	"os"

	"github.com/maruel/go-lepton/gray14"
	"github.com/maruel/go-lepton/lepton"
	"periph.io/x/periph/conn/i2c/i2creg"
	"periph.io/x/periph/conn/spi/spireg"
	"periph.io/x/periph/host"
)

func mainImpl() error {
	i2cName := flag.String("i2c", "", "I²C bus to use")
	spiName := flag.String("spi", "", "SPI bus to use")
	i2cHz := flag.Int("i2chz", 0, "I²C bus speed")
	spiHz := flag.Int("spihz", 0, "SPI bus speed")
	agc := flag.Bool("agc", false, "Save a 8 bit PNG instead of the default 16 bits")
	meta := flag.Bool("meta", false, "print metadata")
	verbose := flag.Bool("v", false, "verbose mode")
	flag.Parse()
	if !*verbose {
		log.SetOutput(ioutil.Discard)
	}
	log.SetFlags(log.Lmicroseconds)

	if flag.NArg() != 1 {
		return errors.New("supply path to PNG to save")
	}

	if _, err := host.Init(); err != nil {
		return err
	}
	spiBus, err := spireg.Open(*spiName)
	if err != nil {
		return err
	}
	defer spiBus.Close()
	if *spiHz != 0 {
		if err := spiBus.LimitSpeed(int64(*spiHz)); err != nil {
			return err
		}
	}

	i2cBus, err := i2creg.Open(*i2cName)
	if err != nil {
		return err
	}
	defer i2cBus.Close()
	if *i2cHz != 0 {
		if err := i2cBus.SetSpeed(int64(*i2cHz)); err != nil {
			return err
		}
	}
	dev, err := lepton.New(spiBus, i2cBus)
	if err != nil {
		return fmt.Errorf("%s\nIf testing without hardware, use -fake to simulate a camera", err)
	}
	frame, err := dev.ReadImg()
	if err != nil {
		return err
	}
	if *meta {
		fmt.Printf("SinceStartup:       %s\n", frame.Metadata.SinceStartup)
		fmt.Printf("FrameCount:         %d\n", frame.Metadata.FrameCount)
		fmt.Printf("Temp:        %s\n", frame.Metadata.Temp)
		fmt.Printf("TempHousing: %s\n", frame.Metadata.TempHousing)
		fmt.Printf("FFCSince:           %s\n", frame.Metadata.FFCSince)
		fmt.Printf("FFCDesired:         %t\n", frame.Metadata.FFCDesired)
		fmt.Printf("Overtemp:           %t\n", frame.Metadata.Overtemp)
	}
	f, err := os.Create(flag.Args()[0])
	if err != nil {
		return err
	}
	var img image.Image = frame
	if *agc {
		img = gray14.AGCLinear(frame.Gray16)
	}
	defer f.Close()
	return png.Encode(f, img)
}

func main() {
	if err := mainImpl(); err != nil {
		fmt.Fprintf(os.Stderr, "\nlepton-grab: %s.\n", err)
		os.Exit(1)
	}
}
