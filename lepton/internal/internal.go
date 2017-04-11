// Copyright 2017 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package internal

import (
	"encoding/binary"
	"time"

	"periph.io/x/periph/devices"
)

// Flag is used in FFCMode.
type Flag uint32

// Valid values for Flag.
const (
	Disabled Flag = 0
	Enabled  Flag = 1
)

// DurationMS is duration in millisecond.
//
// It is an implementation detail of the protocol.
type DurationMS uint32

func (d DurationMS) ToD() time.Duration {
	return time.Duration(d) * time.Millisecond
}

// CentiK is temperature in 0.01°K
//
// It is an implementation detail of the protocol.
type CentiK uint16

func (c CentiK) ToC() devices.Celsius {
	v := (int(c) - 27315) * 10
	return devices.Celsius(v)
}

// Status returns the camera status as returned by the camera.
type Status struct {
	CameraStatus uint32
	CommandCount uint16
	Reserved     uint16
}

// FFCMode
type FFCMode struct {
	FFCShutterMode          uint32     // Default: FFCShutterModeExternal
	ShutterTempLockoutState uint32     // Default: ShutterTempLockoutStateInactive
	VideoFreezeDuringFFC    Flag       // Default: Enabled
	FFCDesired              Flag       // Default: Disabled
	ElapsedTimeSinceLastFFC DurationMS // Uptime in ms.
	DesiredFFCPeriod        DurationMS // Default: 300000
	ExplicitCommandToOpen   Flag       // Default: Disabled
	DesiredFFCTempDelta     uint16     // Default: 300
	ImminentDelay           uint16     // Default: 52

	// These are documented at page 51 but not listed in the structure.
	// ClosePeriodInFrames uint16 // Default: 4
	// OpenPeriodInFrames  uint16 // Default: 1
}

// TelemetryLocation is used with SysTelemetryLocation.
type TelemetryLocation uint32

// Valid values for TelemetryLocation.
const (
	Header TelemetryLocation = 0
	Footer TelemetryLocation = 1
)

// TelemetryRowA is the data structure returned after the frame as documented
// at p.19-20.
//
// '*' means the value observed in practice make sense.
// Value after '-' is observed value.
type TelemetryRowA struct {
	TelemetryRevision  uint16     // 0  *
	TimeCounter        DurationMS // 1  *
	StatusBits         uint32     // 3  * Bit field (mostly make sense)
	ModuleSerial       [16]uint8  // 5  - Is empty (!)
	SoftwareRevision   uint64     // 13   Junk.
	Reserved17         uint16     // 17 - 1101
	Reserved18         uint16     // 18
	Reserved19         uint16     // 19
	FrameCounter       uint32     // 20 *
	FrameMean          uint16     // 22 * The average value from the whole frame.
	FPATempCounts      uint16     // 23
	FPATemp            CentiK     // 24 *
	HousingTempCounts  uint16     // 25
	HousingTemp        CentiK     // 27 *
	Reserved27         uint16     // 27
	Reserved28         uint16     // 28
	FPATempLastFFC     CentiK     // 29 *
	TimeCounterLastFFC DurationMS // 30 *
	HousingTempLastFFC CentiK     // 32 *
	Reserved33         uint16     // 33
	AGCROILeft         uint16     // 35 * - 0 (Likely inversed, haven't confirmed)
	AGCROITop          uint16     // 34 * - 0
	AGCROIRight        uint16     // 36 * - 79 - SDK was wrong!
	AGCROIBottom       uint16     // 37 * - 59 - SDK was wrong!
	AGCClipLimitHigh   uint16     // 38 *
	AGCClipLimitLow    uint16     // 39 *
	Reserved40         uint16     // 40 - 1
	Reserved41         uint16     // 41 - 128
	Reserved42         uint16     // 42 - 64
	Reserved43         uint16     // 43
	Reserved44         uint16     // 44
	Reserved45         uint16     // 45
	Reserved46         uint16     // 46
	Reserved47         uint16     // 47 - 1
	Reserved48         uint16     // 48 - 128
	Reserved49         uint16     // 49 - 1
	Reserved50         uint16     // 50
	Reserved51         uint16     // 51
	Reserved52         uint16     // 52
	Reserved53         uint16     // 53
	Reserved54         uint16     // 54
	Reserved55         uint16     // 55
	Reserved56         uint16     // 56 - 30
	Reserved57         uint16     // 57
	Reserved58         uint16     // 58 - 1
	Reserved59         uint16     // 59 - 1
	Reserved60         uint16     // 60 - 78
	Reserved61         uint16     // 61 - 58
	Reserved62         uint16     // 62 - 7
	Reserved63         uint16     // 63 - 90
	Reserved64         uint16     // 64 - 40
	Reserved65         uint16     // 65 - 210
	Reserved66         uint16     // 66 - 255
	Reserved67         uint16     // 67 - 255
	Reserved68         uint16     // 68 - 23
	Reserved69         uint16     // 69 - 6
	Reserved70         uint16     // 70
	Reserved71         uint16     // 71
	Reserved72         uint16     // 72 - 7
	Reserved73         uint16     // 73
	Log2FFCFrames      uint16     // 74 Found 3, should be 27?
	Reserved75         uint16     // 75
	Reserved76         uint16     // 76
	Reserved77         uint16     // 77
	Reserved78         uint16     // 78
	Reserved79         uint16     // 79
}

//

type table [256]uint16

const ccittFalse = 0x1021

var ccittFalseTable table

func init() {
	makeReversedTable(ccittFalse, &ccittFalseTable)
}

func makeReversedTable(poly uint16, t *table) {
	width := uint16(16)
	for i := uint16(0); i < 256; i++ {
		crc := i << (width - 8)
		for j := 0; j < 8; j++ {
			if crc&(1<<(width-1)) != 0 {
				crc = (crc << 1) ^ poly
			} else {
				crc <<= 1
			}
		}
		t[i] = crc
	}
}

func updateReversed(crc uint16, t *table, p []byte) uint16 {
	for _, v := range p {
		crc = t[byte(crc>>8)^v] ^ (crc << 8)
	}
	return crc
}

// CRC16 calculates the reversed CCITT CRC16 checksum.
func CRC16(d []byte) uint16 {
	return updateReversed(0, &ccittFalseTable, d)
}

//

// Big16 translates big endian 16bits words but everything larger is in little
// endian.
//
// It implements binary.ByteOrder.
var Big16 big16

type big16 struct{}

func (big16) Uint16(b []byte) uint16 {
	_ = b[1] // bounds check hint to compiler; see golang.org/issue/14808
	return uint16(b[1]) | uint16(b[0])<<8
}

func (big16) PutUint16(b []byte, v uint16) {
	_ = b[1] // early bounds check to guarantee safety of writes below
	b[0] = byte(v >> 8)
	b[1] = byte(v)
}

func (big16) Uint32(b []byte) uint32 {
	_ = b[3] // bounds check hint to compiler; see golang.org/issue/14808
	return uint32(b[1]) | uint32(b[0])<<8 | uint32(b[3])<<16 | uint32(b[2])<<24
}

func (big16) PutUint32(b []byte, v uint32) {
	_ = b[3] // early bounds check to guarantee safety of writes below
	b[1] = byte(v)
	b[0] = byte(v >> 8)
	b[3] = byte(v >> 16)
	b[2] = byte(v >> 24)
}

func (big16) Uint64(b []byte) uint64 {
	_ = b[7] // bounds check hint to compiler; see golang.org/issue/14808
	return uint64(b[1]) | uint64(b[0])<<8 | uint64(b[3])<<16 | uint64(b[2])<<24 |
		uint64(b[5])<<32 | uint64(b[4])<<40 | uint64(b[7])<<48 | uint64(b[6])<<56
}

func (big16) PutUint64(b []byte, v uint64) {
	_ = b[7] // early bounds check to guarantee safety of writes below
	b[1] = byte(v)
	b[0] = byte(v >> 8)
	b[3] = byte(v >> 16)
	b[2] = byte(v >> 24)
	b[5] = byte(v >> 32)
	b[4] = byte(v >> 40)
	b[7] = byte(v >> 48)
	b[6] = byte(v >> 56)
}

func (big16) String() string {
	return "big16"
}

var _ binary.ByteOrder = Big16
