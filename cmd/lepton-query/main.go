// Copyright 2017 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

// lepton-query uses its the I²C interface to query its internal state.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/maruel/go-lepton/lepton/cci"

	"periph.io/x/periph/conn/i2c/i2creg"
	"periph.io/x/periph/host"
)

func mainImpl() error {
	i2cName := flag.String("i2c", "", "I²C bus to use")
	i2cHz := flag.Int("hz", 0, "I²C bus speed")
	ffc := flag.Bool("ffc", false, "trigger FFC")
	flag.Parse()

	if len(flag.Args()) != 0 {
		return fmt.Errorf("unexpected argument: %s", flag.Args())
	}

	if _, err := host.Init(); err != nil {
		return err
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
	dev, err := cci.New(i2cBus)
	if err != nil {
		return err
	}
	status, err := dev.GetStatus()
	if err != nil {
		return err
	}
	fmt.Printf("Status.CameraStatus: %s\n", status.CameraStatus)
	fmt.Printf("Status.CommandCount: %d\n", status.CommandCount)
	serial, err := dev.GetSerial()
	if err != nil {
		return err
	}
	fmt.Printf("Serial:              0x%x\n", serial)
	uptime, err := dev.GetUptime()
	if err != nil {
		return err
	}
	fmt.Printf("Uptime:              %s\n", uptime)
	temp, err := dev.GetTemp()
	if err != nil {
		return err
	}
	fmt.Printf("Temp:         %s\n", temp)
	temp, err = dev.GetTempHousing()
	if err != nil {
		return err
	}
	fmt.Printf("Temp housing: %s\n", temp)
	pos, err := dev.GetShutterPos()
	if err != nil {
		return err
	}
	fmt.Printf("ShutterPos:     %s\n", pos)
	mode, err := dev.GetFFCModeControl()
	if err != nil {
		return err
	}
	fmt.Printf("FCCMode.FFCShutterMode:          %s\n", mode.FFCShutterMode)
	fmt.Printf("FCCMode.ShutterTempLockoutState: %s\n", mode.ShutterTempLockoutState)
	fmt.Printf("FCCMode.VideoFreezeDuringFFC:    %t\n", mode.VideoFreezeDuringFFC)
	fmt.Printf("FCCMode.FFCDesired:              %t\n", mode.FFCDesired)
	fmt.Printf("FCCMode.ElapsedTimeSinceLastFFC: %s\n", mode.ElapsedTimeSinceLastFFC)
	fmt.Printf("FCCMode.DesiredFFCPeriod:        %s\n", mode.DesiredFFCPeriod)
	fmt.Printf("FCCMode.ExplicitCommandToOpen:   %t\n", mode.ExplicitCommandToOpen)
	fmt.Printf("FCCMode.DesiredFFCTempDelta:     %s\n", mode.DesiredFFCTempDelta)
	fmt.Printf("FCCMode.ImminentDelay:           %d\n", mode.ImminentDelay)
	if *ffc {
		return dev.RunFFC()
	}
	return nil
}

func main() {
	if err := mainImpl(); err != nil {
		fmt.Fprintf(os.Stderr, "\nlepton-query: %s.\n", err)
		os.Exit(1)
	}
}
