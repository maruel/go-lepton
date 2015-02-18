// Copyright 2015 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package lepton

import (
	"fmt"
	"image"
	"image/color"
	"time"
)

// CentiK is temperature in 0.01°K
type CentiK uint16

func (c CentiK) String() string {
	return fmt.Sprintf("%01d.%02d°K", c/100, c%100)
}

func (c CentiK) ToC() CentiC {
	return CentiC(int(c) - 27315)
}

// CentiC is temperature in 0.01°C. Use 32 bits because otherwise the limit
// would be 327°C, which is a tad too low.
type CentiC int32

func (c CentiC) String() string {
	return fmt.Sprintf("%01d.%02d°C", c/100, c%100)
}

func (c CentiC) ToK() CentiK {
	return CentiK(int(c) + 27315)
}

// FFCState describes the Flat-Field Correction state.
type FFCState uint8

const (
	// No FFC was requested.
	FFCNever FFCState = 0
	// FFC is in progress. It lasts 23 frames (at 27fps) so it lasts less than a second.
	FFCInProgress FFCState = 1
	// FFC was completed successfully.
	FFCComplete FFCState = 2
)

type Metadata struct {
	Raw                   [160]uint8 // (To remove)
	DeviceSerial          [16]uint8
	DeviceVersion         uint64
	SinceStartup          time.Duration
	FFCSince              time.Duration // Time since last FFC.
	FrameCount            uint32
	Temperature           CentiC
	TemperatureHousing    CentiC
	FFCTemperature        CentiC
	FFCTemperatureHousing CentiC
	Revision              uint16 // Header revision. (To remove)
	Mean                  uint16
	RawTemperature        uint16
	RawTemperatureHousing uint16
	FFCLog2               uint16
	FFCState              FFCState
	FFCDesired            bool   // Asserted at start-up, after period (default 3m) or after temperature change (default 3°K). Indicates that an FFC should be triggered as soon as possible.
	AGCEnabled            bool   // true if enabled.
	Overtemp              bool   // true 10s before self-shutdown.
	Min                   uint16 // Manually calculated.
	Max                   uint16
}

// Image implements image.Image. It is essentially a Gray16 but faster
// since the Raspberry Pi is CPU constrained.
// Values centered around 8192 accorgind to camera body temperature. Effective
// range is 14 bits, so [0, 16383].
// Each 1 increment is approximatively 0.025°K.
type LeptonBuffer struct {
	Pix [80 * 60]uint16 // 9600 bytes.
	Metadata
	Telemetry TelemetryRowA
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

// AGCGrayLinear reduces the dynamic range of a 14 bits down to 8 bits very
// naively without gamma.
func (l *LeptonBuffer) AGCGrayLinear(dst *image.Gray) {
	if dst.Rect.Min.X != 0 || dst.Rect.Min.Y != 0 || dst.Rect.Max.X != 80 || dst.Rect.Max.Y != 60 {
		panic("invalid image format")
	}
	floor := l.Min
	delta := int(l.Max - floor)
	for y := 0; y < 60; y++ {
		for x := 0; x < 80; x++ {
			v := int(l.Gray16At(x, y)-floor) * 255 / delta
			dst.Pix[y*80+x] = uint8(v)
		}
	}
}

// Gray14ToRGB converts the image into a RGB with pseudo-colors.
//
// Uses 9bits long palette (512) centered around 8192 for a total range of 512.
// TODO(maruel): Confirm it's real.
// With room temperature of 20C° and precision per unit of 0.025°K, range is
// 512*0.025 = 12.8. (?)
func Gray14ToRGB(intensity uint16) color.NRGBA {
	// Range is [-255, 255].
	i := (int(intensity) - 8192)
	if i < 0 {
		// Use gray scale, further cut the precision by 33% to scale [0, 171].
		if i <= -256 {
			i = -255
		}
		y := uint8((255 - i + 2) * 2 / 3)
		cb := uint8(0)
		cr := uint8(0)
		r, g, b := color.YCbCrToRGB(y, cb, cr)
		return color.NRGBA{r, g, b, 255}
	}
	// Use color. The palette slowly saturates then circle on the hue then
	// increases brightness.
	if i > 256 {
		i = 255
	}
	const base = 255 - (255+2)*2/3
	// Slowly increase brightness.
	y := uint8((i+2)/3 + base)
	cb := uint8(i - 255)
	cr := uint8(255 - i)
	r, g, b := color.YCbCrToRGB(y, cb, cr)
	return color.NRGBA{r, g, b, 255}
}

// PseudoColor reduces the dynamic range of a 14 bits down to RGB. It doesn't
// apply AGC.
func (l *LeptonBuffer) PseudoColor(dst *image.NRGBA) {
	if dst.Rect.Min.X != 0 || dst.Rect.Min.Y != 0 || dst.Rect.Max.X != 80 || dst.Rect.Max.Y != 60 {
		panic("invalid image format")
	}
	for y := 0; y < 60; y++ {
		for x := 0; x < 80; x++ {
			dst.SetNRGBA(x, y, Gray14ToRGB(l.Gray16At(x, y)))
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
