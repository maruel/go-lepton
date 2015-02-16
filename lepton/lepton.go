// Copyright 2015 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

// Package lepton takes video from FLIR Lepton connected to a Raspberry Pi SPI
// port.
//
// References:
// http://www.pureengineering.com/projects/lepton
//
// FLIR LEPTON® Long Wave Infrared (LWIR) Datasheet
//   https://drive.google.com/file/d/0B3wmCw6bdPqFblZsZ3l4SXM4R28/view
//   p. 7 Sensitivity is below 0.05°C
//   p. 19-21 Telemetry mode; TODO(maruel): Enable.
//   p. 22-24 Radiometry mode; TODO(maruel): Enable.
//   p. 28-35 SPI protocol explanation.
//
// Help group mailing list:
//   https://groups.google.com/d/forum/flir-lepton
//
// Connecting to a Raspberry Pi:
//   https://github.com/PureEngineering/LeptonModule/wiki
//
// Lepton™ Software Interface Description Document (IDD):
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
}

func MakeLepton(path string, speed int) (*Lepton, error) {
	// Max rate supported by FLIR Lepton is 20Mhz. Minimum usable rate is ~4Mhz
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

	i2c, err := MakeI2C()
	defer func() {
		if i2c != nil {
			i2c.Close()
		}
	}()
	if err != nil {
		return nil, err
	}
	if err := i2c.SetAddress(i2cAddress); err != nil {
		return nil, err
	}
	// Send a ping to ensure the device is working.
	stat := make([]byte, 8)
	if err := i2c.Cmd(i2cSysStatus, nil, stat); err != nil {
		return nil, err
	}
	log.Printf("i2c status: %v", bytesToStatus(stat))

	out := &Lepton{spi: spi, i2c: i2c, currentLine: -1}
	spi = nil
	i2c = nil
	return out, nil
}

type lepStatus struct {
	camStatus    uint32
	commandCount uint16
	reserved     uint16
}

func bytesToStatus(p []byte) lepStatus {
	return lepStatus{
		camStatus:    uint32(p[0])<<24 | uint32(p[1])<<16 | uint32(p[2])<<8 | uint32(p[3]),
		commandCount: uint16(p[4])<<8 | uint16(p[5]),
		reserved:     uint16(p[6])<<8 | uint16(p[7]),
	}
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
		for l.currentLine != 59 {
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

// Lepton commands
const (
	i2cAddress   = 0x2A
	i2cSysStatus = 0x0204
)

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
	if line > 60 {
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
	for x := 0; x < 80; x++ {
		l.currentImg.Pix[line*80+x] = (uint16(l.packet[2*x+4])<<8 | uint16(l.packet[2*x+5]))
	}
}
