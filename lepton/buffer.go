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

// DiffGray encodes the difference in the image as a 8 bit image centered at
// 128.
func (l *LeptonBuffer) DiffGray(r *LeptonBuffer) *image.Gray {
	dst := image.NewGray(image.Rect(0, 0, 80, 60))
	for y := 0; y < 60; y++ {
		base := y * 80
		for x := 0; x < 80; x++ {
			i := int(l.Gray16At(x, y)) - int(r.Gray16At(x, y))
			if i > 127 {
				i = 127
			} else if i < -128 {
				i = -128
			}
			dst.Pix[base+x] = uint8(i + 128)
		}
	}
	return dst
}

// DiffRGB encodes the difference in the image as an RGB image.
func (l *LeptonBuffer) DiffRGB(r *LeptonBuffer) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, 80, 60))
	for y := 0; y < 60; y++ {
		for x := 0; x < 80; x++ {
			i := int(l.Gray16At(x, y)) - int(r.Gray16At(x, y))
			if i > 127 {
				i = 127
			} else if i < -128 {
				i = -128
			}
			dstBase := 4 * (y*80 + x)
			palBase := 3 * (i + 128)
			dst.Pix[dstBase] = palette[palBase]
			dst.Pix[dstBase+1] = palette[palBase+1]
			dst.Pix[dstBase+2] = palette[palBase+2]
			dst.Pix[dstBase+3] = 255
		}
	}
	return dst
}

// AGCGrayLinear reduces the dynamic range of a 14 bits down to 8 bits very
// naively without gamma.
func (l *LeptonBuffer) AGCGrayLinear() *image.Gray {
	dst := image.NewGray(image.Rect(0, 0, 80, 60))
	floor := l.Min
	delta := int(l.Max - floor)
	for y := 0; y < 60; y++ {
		base := y * 80
		for x := 0; x < 80; x++ {
			dst.Pix[base+x] = uint8(int(l.Gray16At(x, y)-floor) * 255 / delta)
		}
	}
	return dst
}

// AGCGrayLinear reduces the dynamic range of a 14 bits down to 8 bits very
// naively without gamma on a colorful palette.
func (l *LeptonBuffer) AGCRGBLinear() *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, 80, 60))
	floor := l.Min
	delta := int(l.Max - floor)
	for y := 0; y < 60; y++ {
		for x := 0; x < 80; x++ {
			dstBase := 4 * (y*80 + x)
			palBase := 3 * (int(l.Gray16At(x, y)-floor) * 255 / delta)
			dst.Pix[dstBase] = palette[palBase]
			dst.Pix[dstBase+1] = palette[palBase+1]
			dst.Pix[dstBase+2] = palette[palBase+2]
			dst.Pix[dstBase+3] = 255
		}
	}
	return dst
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
func (l *LeptonBuffer) PseudoColor() *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, 80, 60))
	for y := 0; y < 60; y++ {
		for x := 0; x < 80; x++ {
			dst.SetNRGBA(x, y, Gray14ToRGB(l.Gray16At(x, y)))
		}
	}
	return dst
}

func (l *LeptonBuffer) Equal(r *LeptonBuffer) bool {
	for y := 0; y < 60; y++ {
		base := y * 80
		for x := 0; x < 80; x++ {
			if l.Pix[base+x] != r.Pix[base+x] {
				return false
			}
		}
	}
	return true
}

func PaletteGray(vertical bool) *image.Gray {
	x, y := 256, 1
	if vertical {
		x, y = y, x
	}
	dst := image.NewGray(image.Rect(0, 0, x, y))
	for x := 0; x < 256; x++ {
		dst.Pix[x] = uint8(x)
	}
	return dst
}

func PaletteRGB(vertical bool) *image.NRGBA {
	x, y := 256, 1
	if vertical {
		x, y = y, x
	}
	dst := image.NewNRGBA(image.Rect(0, 0, x, y))
	for x := 0; x < 256; x++ {
		dstBase := 4 * x
		palBase := 3 * x
		dst.Pix[dstBase] = palette[palBase]
		dst.Pix[dstBase+1] = palette[palBase+1]
		dst.Pix[dstBase+2] = palette[palBase+2]
		dst.Pix[dstBase+3] = 255
	}
	return dst
}

// Private details.

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

var palette = []uint8{
	255, 255, 255, 253, 253, 253, 251, 251, 251, 249, 249, 249, 247, 247, 247,
	245, 245, 245, 243, 243, 243, 241, 241, 241, 239, 239, 239, 237, 237, 237,
	235, 235, 235, 233, 233, 233, 231, 231, 231, 229, 229, 229, 227, 227, 227,
	225, 225, 225, 223, 223, 223, 221, 221, 221, 219, 219, 219, 217, 217, 217,
	215, 215, 215, 213, 213, 213, 211, 211, 211, 209, 209, 209, 207, 207, 207,
	205, 205, 205, 203, 203, 203, 201, 201, 201, 199, 199, 199, 197, 197, 197,
	195, 195, 195, 193, 193, 193, 191, 191, 191, 189, 189, 189, 187, 187, 187,
	185, 185, 185, 183, 183, 183, 181, 181, 181, 179, 179, 179, 177, 177, 177,
	175, 175, 175, 173, 173, 173, 171, 171, 171, 169, 169, 169, 167, 167, 167,
	165, 165, 165, 163, 163, 163, 161, 161, 161, 159, 159, 159, 157, 157, 157,
	155, 155, 155, 153, 153, 153, 151, 151, 151, 149, 149, 149, 147, 147, 147,
	145, 145, 145, 143, 143, 143, 141, 141, 141, 139, 139, 139, 137, 137, 137,
	135, 135, 135, 133, 133, 133, 131, 131, 131, 129, 129, 129, 126, 126, 126,
	124, 124, 124, 122, 122, 122, 120, 120, 120, 118, 118, 118, 116, 116, 116,
	114, 114, 114, 112, 112, 112, 110, 110, 110, 108, 108, 108, 106, 106, 106,
	104, 104, 104, 102, 102, 102, 100, 100, 100, 98, 98, 98, 96, 96, 96, 94, 94,
	94, 92, 92, 92, 90, 90, 90, 88, 88, 88, 86, 86, 86, 84, 84, 84, 82, 82, 82,
	80, 80, 80, 78, 78, 78, 76, 76, 76, 74, 74, 74, 72, 72, 72, 70, 70, 70, 68,
	68, 68, 66, 66, 66, 64, 64, 64, 62, 62, 62, 60, 60, 60, 58, 58, 58, 56, 56,
	56, 54, 54, 54, 52, 52, 52, 50, 50, 50, 48, 48, 48, 46, 46, 46, 44, 44, 44,
	42, 42, 42, 40, 40, 40, 38, 38, 38, 36, 36, 36, 34, 34, 34, 32, 32, 32, 30,
	30, 30, 28, 28, 28, 26, 26, 26, 24, 24, 24, 22, 22, 22, 20, 20, 20, 18, 18,
	18, 16, 16, 16, 14, 14, 14, 12, 12, 12, 10, 10, 10, 8, 8, 8, 6, 6, 6, 4, 4,
	4, 2, 2, 2, 0, 0, 0, 0, 0, 9, 2, 0, 16, 4, 0, 24, 6, 0, 31, 8, 0, 38, 10, 0,
	45, 12, 0, 53, 14, 0, 60, 17, 0, 67, 19, 0, 74, 21, 0, 82, 23, 0, 89, 25, 0,
	96, 27, 0, 103, 29, 0, 111, 31, 0, 118, 36, 0, 120, 41, 0, 121, 46, 0, 122,
	51, 0, 123, 56, 0, 124, 61, 0, 125, 66, 0, 126, 71, 0, 127, 76, 1, 128, 81,
	1, 129, 86, 1, 130, 91, 1, 131, 96, 1, 132, 101, 1, 133, 106, 1, 134, 111, 1,
	135, 116, 1, 136, 121, 1, 136, 125, 2, 137, 130, 2, 137, 135, 3, 137, 139, 3,
	138, 144, 3, 138, 149, 4, 138, 153, 4, 139, 158, 5, 139, 163, 5, 139, 167, 5,
	140, 172, 6, 140, 177, 6, 140, 181, 7, 141, 186, 7, 141, 189, 10, 137, 191,
	13, 132, 194, 16, 127, 196, 19, 121, 198, 22, 116, 200, 25, 111, 203, 28,
	106, 205, 31, 101, 207, 34, 95, 209, 37, 90, 212, 40, 85, 214, 43, 80, 216,
	46, 75, 218, 49, 69, 221, 52, 64, 223, 55, 59, 224, 57, 49, 225, 60, 47, 226,
	64, 44, 227, 67, 42, 228, 71, 39, 229, 74, 37, 230, 78, 34, 231, 81, 32, 231,
	85, 29, 232, 88, 27, 233, 92, 24, 234, 95, 22, 235, 99, 19, 236, 102, 17,
	237, 106, 14, 238, 109, 12, 239, 112, 12, 240, 116, 12, 240, 119, 12, 241,
	123, 12, 241, 127, 12, 242, 130, 12, 242, 134, 12, 243, 138, 12, 243, 141,
	13, 244, 145, 13, 244, 149, 13, 245, 152, 13, 245, 156, 13, 246, 160, 13,
	246, 163, 13, 247, 167, 13, 247, 171, 13, 248, 175, 14, 248, 178, 15, 249,
	182, 16, 249, 185, 18, 250, 189, 19, 250, 192, 20, 251, 196, 21, 251, 199,
	22, 252, 203, 23, 252, 206, 24, 253, 210, 25, 253, 213, 27, 254, 217, 28,
	254, 220, 29, 255, 224, 30, 255, 227, 39, 255, 229, 53, 255, 231, 67, 255,
	233, 81, 255, 234, 95, 255, 236, 109, 255, 238, 123, 255, 240, 137, 255, 242,
	151, 255, 244, 165, 255, 246, 179, 255, 248, 193, 255, 249, 207, 255, 251,
	221, 255, 253, 235, 255, 255, 24,
}

func init() {
	if len(palette) != 3*256 {
		panic(fmt.Sprintf("expected %d, got %d", 3*256, len(palette)))
	}
}
