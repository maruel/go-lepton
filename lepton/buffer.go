// Copyright 2015 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package lepton

import (
	"image"
	"image/color"
	"time"
)

// CentiK is temperature in 0.01K
type CentiK uint16

type FCCState uint8

const (
	FCCNever      = FCCState(0)
	FCCInProgress = FCCState(1)
	FCCComplete   = FCCState(2)
)

type Metadata struct {
	DeviceSerial          [16]uint8
	DeviceVersion         uint64
	SinceStartup          time.Duration
	FCCSince              time.Duration // Time since last FCC.
	FrameCount            uint32
	Mean                  uint16
	RawTemperature        uint16
	Temperature           CentiK
	RawTemperatureHousing uint16
	TemperatureHousing    CentiK
	FCCTemperature        CentiK
	FCCTemperatureHousing CentiK
	FCCLog2               uint16
	FCCState              FCCState
	FCCDesired            bool
	AGCEnabled            bool   // true if enabled.
	Overtemp              bool   // true 10s before self-shutdown.
	Min                   uint16 // Manually calculated.
	Max                   uint16
}

// Image implements image.Image. It is essentially a Gray16 but faster
// since the Raspberry Pi is CPU constrained.
type LeptonBuffer struct {
	Pix [80 * 60]uint16 // 9600 bytes.
	Metadata
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

// AGC reduces the dynamic range of a 14 bits down to 8 bits very naively
// without gamma.
func (l *LeptonBuffer) AGC(dst *image.Gray) {
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

func (l *LeptonBuffer) Equal(r *LeptonBuffer) bool {
	for y := 0; y < 60; y++ {
		for x := 0; x < 80; x++ {
			if l.Pix[y*80+x] != r.Pix[y*80+x] {
				return false
			}
		}
	}
	return true
}
