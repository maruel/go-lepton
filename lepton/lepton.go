// Copyright 2015 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

// Package lepton takes video from FLIR Lepton connected to a Raspberry Pi SPI
// port.
//
// References:
// Official FLIR Lepton FAQ:
//   http://www.flir.com/cvs/cores/knowledgebase/browse.cfm?CFTREEITEMKEY=914
//
// DIY:
//   http://www.pureengineering.com/projects/lepton
//
// FLIR LEPTON® Long Wave Infrared (LWIR) Datasheet
//   https://drive.google.com/file/d/0B3wmCw6bdPqFblZsZ3l4SXM4R28/view
//   p. 7 Sensitivity is below 0.05°C
//   p. 19-21 Telemetry mode
//   p. 22-24 Radiometry mode; TODO(maruel): Enable.
//   p. 28-35 SPI protocol explanation.
//
// Help group mailing list:
//   https://groups.google.com/d/forum/flir-lepton
//
// Connecting to a Raspberry Pi:
//   https://github.com/PureEngineering/LeptonModule/wiki
//
// Lepton™ Software Interface Description Document (IDD) for i²c protocol:
//   (Application level and SDK doc)
//   https://drive.google.com/file/d/0B3wmCw6bdPqFOHlQbExFbWlXS0k/view
//   p. 24    i²c command format.
//   p. 36-37 Ping and Status, implement first to ensure i²c works.
//   p. 42-43 Telemetry enable.
//   Radiometry is not documented (!?)
//
// Information about the Raspberry Pi SPI driver:
//   http://elinux.org/RPi_SPI
package lepton

import (
	"errors"
	"fmt"
	"log"
	"os"
	"time"
)

type CameraStatus uint32

// Valid values for Status.
const (
	SystemReady              = CameraStatus(0)
	SystemInitializing       = CameraStatus(1)
	SystemInLowPowerMode     = CameraStatus(2)
	SystemGoingIntoStandby   = CameraStatus(3)
	SystemFlatFieldInProcess = CameraStatus(4)
)

type Stats struct {
	LastFail        error
	GoodFrames      int
	DuplicateFrames int
	TransferFails   int
	BrokenPackets   int
	SyncFailures    int
	DummyLines      int
}

type Lepton struct {
	spi         *SPI
	i2c         *I2C
	currentImg  *LeptonBuffer
	currentLine int
	packet      [164]uint8 // one line is sent as a SPI packet.
	stats       Stats
	serial      uint64
}

func MakeLepton(path string, speed int) (*Lepton, error) {
	// Max rate supported by FLIR Lepton is 25Mhz. Minimum usable rate is ~4Mhz
	// to sustain framerate. Sadly the Lepton will inconditionally send 27fps,
	// even if the effective rate is 9fps. Lower rate is less likely to get
	// electromagnetic interference and reduces unnecessary CPU consumption by
	// reducing the number of dummy packets.
	if speed == 0 {
		// spi_bcm2708 supports a limited number of frequencies so the actual value
		// will differ. See http://elinux.org/RPi_SPI.
		// Actual rate will be 7.8Mhz or 15.6Mhz.
		// TODO(maruel): Figure out a way to determine if the driver decides to
		// round down or up.
		speed = 7900000
	}
	if speed < 3900000 {
		return nil, errors.New("speed specified is too slow")
	}
	spi, err := MakeSPI(path, speed)
	defer func() {
		if spi != nil {
			spi.Close()
		}
	}()
	if err != nil {
		return nil, err
	}

	// Rated speed is 1Mhz.
	i2c, err := MakeI2C()
	defer func() {
		if i2c != nil {
			i2c.Close()
		}
	}()
	if err != nil {
		return nil, err
	}

	// Send a ping to ensure the device is working.
	out := &Lepton{spi: spi, i2c: i2c, currentLine: -1}
	status, err := out.GetStatus()
	if err != nil {
		return nil, err
	}
	if status.CameraStatus != SystemReady {
		log.Printf("WARNING: camera is not ready: %s", status)
	}
	// Warning: Assumes AGC is disabled. There's no code here to enable it anyway.
	if err := i2c.SetAttribute(SysTelemetryEnable, []uint16{0, 1}); err != nil {
		return nil, err
	}
	spi = nil
	i2c = nil

	return out, nil
}

type Status struct {
	CameraStatus CameraStatus
	CommandCount uint16
	Reserved     uint16
}

func (l *Lepton) GetStatus() (*Status, error) {
	p := make([]uint16, 4)
	if err := l.i2c.GetAttribute(SysStatus, p); err != nil {
		return nil, err
	}
	return &Status{
		CameraStatus: CameraStatus(uint32(p[1])<<16 | uint32(p[0])),
		CommandCount: p[2],
		Reserved:     p[3],
	}, nil
}

// GetSerial returns the FLIR Lepton serial number.
func (l *Lepton) GetSerial() (uint64, error) {
	if l.serial == 0 {
		p := make([]uint16, 4)
		if err := l.i2c.GetAttribute(SysSerialNumber, p); err != nil {
			return 0, err
		}
		log.Printf("serial: 0x%04x %04x %04x %04x", p[0], p[1], p[2], p[3])
		l.serial = uint64(p[3])<<48 | uint64(p[2])<<32 | uint64(p[1])<<16 | uint64(p[0])
	}
	return l.serial, nil
}

// GetUptime returns the uptime. Rolls over after 1193 hours.
func (l *Lepton) GetUptime() (time.Duration, error) {
	p := []uint16{0, 0}
	if err := l.i2c.GetAttribute(SysUptime, p); err != nil {
		return 0, err
	}
	log.Printf("uptime: 0x%04x %04x", p[0], p[1])
	return time.Duration(uint32(p[1])<<16|uint32(p[0])) * time.Millisecond, nil
}

// GetTemperatureHousing returns the temperature in centi-Kelvin.
func (l *Lepton) GetTemperatureHousing() (CentiK, error) {
	p := []uint16{0}
	if err := l.i2c.GetAttribute(SysHousingTemperature, p); err != nil {
		return 0, err
	}
	log.Printf("temp: 0x%04x", p[0])
	return CentiK(p[0]), nil
}

// GetTemperature returns the temperature in centi-Kelvin.
func (l *Lepton) GetTemperature() (CentiK, error) {
	p := []uint16{0}
	if err := l.i2c.GetAttribute(SysTemperature, p); err != nil {
		return 0, err
	}
	log.Printf("temp: 0x%04x", p[0])
	return CentiK(p[0]), nil
}

func (l *Lepton) Close() error {
	var err error
	if l.spi != nil {
		err = l.spi.Close()
		l.spi = nil
	}
	if l.i2c != nil {
		err = l.i2c.Close()
		l.i2c = nil
	}
	return err
}

func (l *Lepton) Stats() Stats {
	// TODO(maruel): atomic.
	return l.stats
}

// ReadImg reads an image into an image. It must be 80x60.
func (l *Lepton) ReadImg(r *LeptonBuffer) {
	l.currentLine = -1
	prevImg := l.currentImg
	l.currentImg = r
	for {
		// TODO(maruel): Fail after N errors?
		// TODO(maruel): Skip 2 frames since they'll be the same data so no need
		// for the check below.
		// Do not forget the 3 telemetry lines.
		for l.currentLine != 62 {
			l.readLine()
		}
		if prevImg == nil || !prevImg.Equal(l.currentImg) {
			l.stats.GoodFrames++
			l.currentImg.updateStats()
			break
		}
		// It also happen if the image is 100% static without noise.
		l.stats.DuplicateFrames++
		l.currentLine = -1
	}
}

// Private details.

// readLine reads one line at a time.
//
// Each line is sent as a packet over SPI. The packet size is constant. See
// page 28-35 for SPI protocol explanation.
// https://drive.google.com/file/d/0B3wmCw6bdPqFblZsZ3l4SXM4R28/view
func (l *Lepton) readLine() {
	// Operation must complete within 32ms. Frames occur every 38.4ms. With SPI,
	// write must occur as read is being done, just sent dummy data.
	n, err := l.spi.Read(l.packet[:])
	if n != len(l.packet) && err == nil {
		err = fmt.Errorf("unexpected read %d", n)
	}
	if err != nil {
		l.stats.TransferFails++
		l.currentLine = -1
		if l.stats.LastFail == nil {
			fmt.Fprintf(os.Stderr, "\nI/O fail: %s\n", err)
			l.stats.LastFail = err
		}
		time.Sleep(200 * time.Millisecond)
		return
	}

	l.stats.LastFail = nil
	if (l.packet[0] & 0xf) == 0x0f {
		// Discard packet. This happens as the bandwidth of SPI is larger than data
		// rate.
		l.currentLine = -1
		l.stats.DummyLines++
		return
	}

	// If out of sync, Deassert /CS and idle SCK for at least 5 frame periods
	// (>185ms).

	// TODO(maruel): Verify CRC (bytes 2-3) ?
	line := int(l.packet[1])
	if line >= 63 {
		time.Sleep(200 * time.Millisecond)
		l.stats.BrokenPackets++
		l.currentLine = -1
		return
	}
	if line != l.currentLine+1 {
		time.Sleep(200 * time.Millisecond)
		l.stats.SyncFailures++
		l.currentLine = -1
		return
	}

	// Convert the line from byte to uint16. 14 bits significant.
	l.currentLine++
	if line < 60 {
		for x := 0; x < 80; x++ {
			l.currentImg.Pix[line*80+x] = (uint16(l.packet[2*x+4])<<8 | uint16(l.packet[2*x+5]))
		}
	} else if line == 60 {
		// Telemetry line.
		// Everything is in big endian uint16.
		offset := func(x int) uint16 {
			return uint16(l.packet[2*x+4])<<8 | uint16(l.packet[2*x+5])
		}
		revision := offset(0)
		// Ensures the revision is known, remove once it's known to work.
		if revision != 8 && revision != 9 {
			panic(fmt.Errorf("Unexpected revision 0x%X", revision))
		}
		l.currentImg.SinceStartup = time.Duration(uint32(offset(1))<<16|uint32(offset(2))) * time.Millisecond

		status := uint32(offset(3))<<16 | uint32(offset(4))
		// Ensures all reserved bits are 0, just to confirm we didn't mess up. Remove this code once it's proved to be working.
		if status&0xffefefc7 != 0 {
			panic(fmt.Errorf("Unexpected status 0x%X", status))
		}
		l.currentImg.FCCDesired = status&(1<<3) != 0
		fccstate := (status & (1<<5 + 1<<4)) >> 4
		if revision <= 8 {
			switch fccstate {
			case 0:
				l.currentImg.FCCState = FCCNever
			case 1:
				l.currentImg.FCCState = FCCInProgress
			case 2:
				l.currentImg.FCCState = FCCComplete
			default:
				panic(fmt.Errorf("unexpected fccstate %d", fccstate))
			}
		} else {
			switch fccstate {
			case 0:
				l.currentImg.FCCState = FCCNever
			case 2:
				l.currentImg.FCCState = FCCInProgress
			case 3:
				l.currentImg.FCCState = FCCComplete
			default:
				panic(fmt.Errorf("unexpected fccstate %d", fccstate))
			}
		}
		// Should never be enabled.
		l.currentImg.AGCEnabled = status&(1<<12) != 0
		l.currentImg.Overtemp = status&(1<<20) != 0

		copy(l.currentImg.DeviceSerial[:], l.packet[5:8])
		l.currentImg.DeviceVersion = uint64(offset(13))<<48 | uint64(offset(14))<<32 | uint64(offset(15))<<16 | uint64(offset(16))
		l.currentImg.FrameCount = uint32(offset(20))<<16 | uint32(offset(21))
		l.currentImg.Mean = offset(22)
		l.currentImg.RawTemperature = offset(23)
		l.currentImg.Temperature = CentiK(offset(24))
		l.currentImg.RawTemperatureHousing = offset(25)
		l.currentImg.TemperatureHousing = CentiK(offset(26))
		l.currentImg.FCCTemperature = CentiK(offset(29))
		l.currentImg.FCCSince = time.Duration(uint32(offset(30))<<16|uint32(offset(31))) * time.Millisecond
		l.currentImg.FCCTemperatureHousing = CentiK(offset(32))
		l.currentImg.FCCLog2 = offset(74)
	}
}
