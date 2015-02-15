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
//   p. 36-37 Ping and Status, implement first to ensure i²c works.
//   p. 42-43 Telemetry enable.
//   Radiometry is not documented (!?)
//
// Information about the Raspberry Pi SPI driver:
//   http://elinux.org/RPi_SPI
package lepton

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"syscall"
	"time"
	"unsafe"
)

const (
	spiIOCWrMode        = 0x40016B01
	spiIOCWrBitsPerWord = 0x40016B03
	spiIOCWrMaxSpeedHz  = 0x40046B04

	spiIOCRdMode        = 0x80016B01
	spiIOCRdBitsPerWord = 0x80016B03
	spiIOCRdMaxSpeedHz  = 0x80046B04
)

// LeptonBuffer implements image.Image. It is essentially a Gray16 but faster
// since the Raspberry Pi is CPU constrained.
type LeptonBuffer struct {
	Pix [80 * 60]uint16 // 9600 bytes.
	Min uint16
	Max uint16
}

func (l *LeptonBuffer) ColorModel() color.Model {
	return color.Gray16Model
}

func (l *LeptonBuffer) Bounds() image.Rectangle {
	return image.Rect(0, 0, 80, 60)
}

func (l *LeptonBuffer) At(x, y int) color.Color {
	return color.Gray16{l.Gray16At(x, y)}
}

func (l *LeptonBuffer) Gray16At(x, y int) uint16 {
	return l.Pix[y*80+x]
}

// Scale reduces the dynamic range of a 14 bits down to 8 bits very naively.
func (l *LeptonBuffer) Scale(dst *image.Gray) {
	floor := l.Min
	delta := int(l.Max - floor)
	for y := 0; y < 60; y++ {
		for x := 0; x < 80; x++ {
			v := int(l.Gray16At(x, y)-floor) * 255 / delta
			dst.Pix[y*80+x] = uint8(v)
		}
	}
}

func (l *LeptonBuffer) updateStats() {
	l.Max = uint16(0)
	l.Min = uint16(0xffff)
	for y := 0; y < 60; y++ {
		for x := 0; x < 80; x++ {
			j := l.Pix[y*80+x]
			if j > l.Max {
				l.Max = j
			}
			if j < l.Min {
				l.Min = j
			}
		}
	}
}

func Eq(l *LeptonBuffer, r *LeptonBuffer) bool {
	for y := 0; y < 60; y++ {
		for x := 0; x < 80; x++ {
			if l.Pix[y*80+x] != r.Pix[y*80+x] {
				return false
			}
		}
	}
	return true
}

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
	f           *os.File
	currentImg  *LeptonBuffer
	currentLine int
	packet      [164]uint8 // one line is sent as a SPI packet.
	stats       Stats
}

func MakeLepton() (*Lepton, error) {
	// Max rate supported by FLIR Lepton is 20Mhz. Minimum usable rate is ~4Mhz
	// to sustain framerate.  Low rate is less likely to get electromagnetic
	// interference and reduces unnecessary CPU consumption by reducing the
	// number of dummy packets. spi_bcm2708 supports a limited number of
	// frequencies so the actual value will differ. See http://elinux.org/RPi_SPI.
	f, err := os.OpenFile("/dev/spidev0.0", os.O_RDWR, os.ModeExclusive)
	if err != nil {
		return nil, err
	}
	out := &Lepton{f: f, currentLine: -1}

	mode := uint8(3)
	if err := out.ioctl(spiIOCWrMode, uintptr(unsafe.Pointer(&mode))); err != nil {
		return out, err
	}
	if err := out.ioctl(spiIOCRdMode, uintptr(unsafe.Pointer(&mode))); err != nil {
		return out, err
	}
	if mode != 3 {
		return out, fmt.Errorf("unexpected mode %d", mode)
	}

	bits := uint8(8)
	if err := out.ioctl(spiIOCWrBitsPerWord, uintptr(unsafe.Pointer(&bits))); err != nil {
		return out, err
	}
	if err := out.ioctl(spiIOCRdBitsPerWord, uintptr(unsafe.Pointer(&bits))); err != nil {
		return out, err
	}
	if bits != 8 {
		return out, fmt.Errorf("unexpected bits %d", bits)
	}

	speed := uint32(8000000)
	if err := out.ioctl(spiIOCWrMaxSpeedHz, uintptr(unsafe.Pointer(&speed))); err != nil {
		return out, err
	}
	if err := out.ioctl(spiIOCRdMaxSpeedHz, uintptr(unsafe.Pointer(&speed))); err != nil {
		return out, err
	}
	if speed != 8000000 {
		return out, fmt.Errorf("unexpected speed %d", bits)
	}

	return out, nil
}

func (l *Lepton) Close() error {
	if l.f != nil {
		err := l.f.Close()
		l.f = nil
		return err
	}
	return nil
}

func (l *Lepton) Stats() Stats {
	// TODO(maruel): atomic.
	return l.stats
}

func (l *Lepton) ioctl(op, arg uintptr) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, l.f.Fd(), op, arg); errno != 0 {
		return syscall.Errno(errno)
	}
	return nil
}

// readLine reads one line at a time.
//
// Each line is sent as a packet over SPI. The packet size is constant. See
// page 28-35 for SPI protocol explanation.
// https://drive.google.com/file/d/0B3wmCw6bdPqFblZsZ3l4SXM4R28/view
func (l *Lepton) readLine() {
	// Operation must complete within 32ms. Frames occur every 38.4ms. With SPI,
	// write must occur as read is being done, just sent dummy data.
	n, err := l.f.Read(l.packet[:])
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
		if prevImg == nil || !Eq(prevImg, l.currentImg) {
			l.stats.GoodFrames++
			l.currentImg.updateStats()
			break
		}
		// It also happen if the image is static.
		l.stats.DuplicateFrames++
		l.currentLine = -1
	}
}
