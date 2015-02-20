// Copyright 2015 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

// Package lepton takes video from FLIR Lepton connected to a Raspberry Pi SPI
// port.
//
// References:
// Official FLIR Lepton page:
//   http://www.flir.com/cores/content/?id=66257
//   http://www.flir.com/cvs/cores/view/?id=51878
//
// Official FLIR Lepton FAQ:
//   http://www.flir.com/cvs/cores/knowledgebase/browse.cfm?CFTREEITEMKEY=914
//
// FLIR LEPTON® Long Wave Infrared (LWIR) Datasheet
//   http://cvs.flir.com/lepton-data-brief
//   https://drive.google.com/file/d/0B3wmCw6bdPqFblZsZ3l4SXM4R28/view (copy)
//   p. 7 Sensitivity is below 0.05°C
//   p. 19-21 Telemetry mode
//   p. 22-24 Radiometry mode; TODO(maruel): Enable.
//   p. 28-35 SPI protocol explanation.
//
// Lepton™ Software Interface Description Document (IDD) for i²c protocol:
//   (Application level and SDK doc)
//   http://cvs.flir.com/lepton-idd
//   https://drive.google.com/file/d/0B3wmCw6bdPqFOHlQbExFbWlXS0k/view (copy)
//   p. 24    i²c command format.
//   p. 36-37 Ping and Status, implement first to ensure i²c works.
//   p. 42-43 Telemetry enable.
//   Radiometry is not documented (!?)
//
// DIY:
//   http://www.pureengineering.com/projects/lepton
//
// Help group mailing list:
//   https://groups.google.com/d/forum/flir-lepton
//
// Connecting to a Raspberry Pi:
//   https://github.com/PureEngineering/LeptonModule/wiki
//
// Information about the Raspberry Pi SPI driver:
//   http://elinux.org/RPi_SPI
package lepton

// "stringer" can be installed with "go get golang.org/x/tools/cmd/stringer"
//go:generate stringer -output=strings_gen.go -type=CameraStatus,Command,FFCShutterMode,FFCState,Flag,RegisterAddress,ShutterTempLockoutState,TelemetryLocation

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"time"
)

// CameraStatus is retrieved via Lepton.GetStatus().
type CameraStatus uint32

// Valid values for CameraStatus.
const (
	SystemReady              CameraStatus = 0
	SystemInitializing       CameraStatus = 1
	SystemInLowPowerMode     CameraStatus = 2
	SystemGoingIntoStandby   CameraStatus = 3
	SystemFlatFieldInProcess CameraStatus = 4
)

// FFCShutterMode is used in FFCMode.
type FFCShutterMode uint32

// Valid values for FFCShutterMode.
const (
	FFCShutterModeManual   FFCShutterMode = 0
	FFCShutterModeAuto     FFCShutterMode = 1
	FFCShutterModeExternal FFCShutterMode = 2
)

// ShutterTempLockoutState is used in FFCMode.
type ShutterTempLockoutState uint32

// Valid values for ShutterTempLockoutState.
const (
	ShutterTempLockoutStateInactive ShutterTempLockoutState = 0
	ShutterTempLockoutStateHigh     ShutterTempLockoutState = 1
	ShutterTempLockoutStateLow      ShutterTempLockoutState = 2
)

// Flag is used in FFCMode.
type Flag uint32

// Valid values for Flag.
const (
	Disabled Flag = 0
	Enabled  Flag = 1
)

// TelemetryLocation is used with SysTelemetryLocation.
type TelemetryLocation uint32

// Valid values for TelemetryLocation.
const (
	Header TelemetryLocation = 0
	Footer TelemetryLocation = 1
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
	d := c % 100
	if d < 0 {
		d = -d
	}
	return fmt.Sprintf("%01d.%02d°C", c/100, d)
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

type Stats struct {
	LastFail        error
	Resets          int
	GoodFrames      int
	DuplicateFrames int
	TransferFails   int
	GoodLines       int
	BrokenLines     int
	DiscardLines    int
	BadSyncLines    int
}

// Status is returned by Lepton.GetStatus().
type Status struct {
	CameraStatus CameraStatus
	CommandCount uint16
	Reserved     uint16
}

// DurationMS is duration in millisecond
type DurationMS uint32

func (d DurationMS) ToDuration() time.Duration {
	return time.Duration(d) * time.Millisecond
}

// FFCMode is returned by Lepton.GetFFCModeControl().
type FFCMode struct {
	FFCShutterMode          FFCShutterMode          // Default: FFCShutterModeExternal
	ShutterTempLockoutState ShutterTempLockoutState // Default: ShutterTempLockoutStateInactive
	VideoFreezeDuringFFC    Flag                    // Default: Enabled
	FFCDesired              Flag                    // Default: Disabled
	ElapsedTimeSinceLastFFC DurationMS              // Uptime in ms.
	DesiredFFCPeriod        DurationMS              // Default: 300000
	ExplicitCommandToOpen   Flag                    // Default: Disabled
	DesiredFFCTempDelta     CentiK                  // Default: 300
	ImminentDelay           uint16                  // Default: 52

	// These are documented at page 51 but not listed in the structure.
	// ClosePeriodInFrames uint16 // Default: 4
	// OpenPeriodInFrames  uint16 // Default: 1
}

// Lepton controls a FLIR Lepton. It assumes a specific breakout board. Sadly
// the breakout board doesn't expose the PWR_DWN_L and RESET_L lines so it is
// impossible to shut down the Lepton.
type Lepton struct {
	spi               *SPI
	i2c               *I2C
	currentImg        *LeptonBuffer
	previousImg       *LeptonBuffer
	currentLine       int
	packet            [164]uint8 // one line is sent as a SPI packet.
	stats             Stats
	serial            uint64
	telemetry         Flag
	telemetryLocation TelemetryLocation
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
		//speed = 15700000
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
	out := &Lepton{spi: spi, i2c: i2c, currentLine: -1, telemetry: Disabled, telemetryLocation: Header}
	status, err := out.GetStatus()
	if err != nil {
		return nil, err
	}
	if status.CameraStatus != SystemReady {
		log.Printf("WARNING: camera is not ready: %s", status)
	}

	// Setup telemetry.
	// Warning: Assumes AGC is disabled. There's no code here to enable it anyway.
	if err := i2c.SetAttribute(SysTelemetryEnable, []uint16{1, 0}); err != nil {
		return nil, err
	}
	// I had trouble using it as footer.
	if err := i2c.SetAttribute(SysTelemetryLocation, []uint16{0, 0}); err != nil {
		return nil, err
	}

	spi = nil
	i2c = nil
	return out, nil
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
func (l *Lepton) GetTemperatureHousing() (CentiC, error) {
	p := []uint16{0}
	if err := l.i2c.GetAttribute(SysHousingTemperature, p); err != nil {
		return 0, err
	}
	log.Printf("temp: 0x%04x", p[0])
	return CentiK(p[0]).ToC(), nil
}

// GetTemperature returns the temperature in centi-Kelvin.
func (l *Lepton) GetTemperature() (CentiC, error) {
	p := []uint16{0}
	if err := l.i2c.GetAttribute(SysTemperature, p); err != nil {
		return 0, err
	}
	log.Printf("temp: 0x%04x", p[0])
	return CentiK(p[0]).ToC(), nil
}

// TriggerFFC forces a Flat-Field Correction to be done by the camera for
// recalibration. It takes 23 frames and the camera runs at 27fps so it lasts
// less than a second.
func (l *Lepton) TriggerFFC() error {
	return l.i2c.RunCommand(SysFCCRunNormalization)
}

func (l *Lepton) Stats() Stats {
	// TODO(maruel): atomic.
	return l.stats
}

// ReadImg reads an image. It is fine to call other functions concurrently to
// send commands to the camera.
func (l *Lepton) ReadImg() *LeptonBuffer {
	l.currentLine = -1
	l.previousImg = l.currentImg
	l.currentImg = &LeptonBuffer{}
	for {
		// TODO(maruel): Fail after N errors?
		// TODO(maruel): Skip 2 frames since they'll be the same data so no need
		// for the check below.
		// Do not forget the 3 telemetry lines.
		for l.currentLine != 62 {
			l.readLine()
		}
		if l.previousImg == nil || !l.previousImg.Equal(l.currentImg) {
			l.stats.GoodFrames++
			l.currentImg.updateStats()
			break
		}
		// It also happen if the image is 100% static without noise.
		l.stats.DuplicateFrames++
		l.currentLine = -1
	}
	return l.currentImg
}

// Private details.

// maxLine returns the last valid VoSPI line.
func (l *Lepton) maxLine() int {
	if l.telemetry != Disabled {
		return 59 + 3
	}
	return 59
}

// realLine returns the image or telemetry line.
func (l *Lepton) realLine(line int) (imgLine int, telemetryLine int) {
	if l.telemetry == Disabled {
		return line, -1
	}
	switch l.telemetryLocation {
	case Header:
		if line < 3 {
			return -1, line
		}
		return line - 3, -1
	case Footer:
		if line > 59 {
			return -1, line - 60
		}
		return line, -1
	default:
		panic("internal error")
	}
}

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
			log.Printf("I/O fail: %s", err)
			l.stats.LastFail = err
		}
		l.stats.Resets++
		l.spi.Reset()
		return
	}

	l.stats.LastFail = nil
	if (l.packet[0] & 0xf) == 0x0f {
		// Discard packet. This happens as the bandwidth of SPI is larger than data
		// rate.
		//l.currentLine = -1
		l.stats.DiscardLines++
		return
	}

	// If out of sync, Deassert /CS and idle SCK for at least 5 frame periods
	// (>185ms).

	// TODO(maruel): Verify CRC (bytes 2-3) ?
	line := int(l.packet[1])
	if line >= 63 {
		log.Printf("got unexpected line %d  %v", line, l.packet)
		time.Sleep(200 * time.Millisecond)
		l.stats.BrokenLines++
		l.currentLine = -1
		return
	}
	if line != l.currentLine+1 {
		l.stats.BadSyncLines++
		if line == l.currentLine {
			log.Printf("duplicate line %d", line)
			return
		}
		if line == 0 {
			// A new frame was started, ignore the previous one.
			log.Printf("reset")
			l.currentLine = -1
		} else if line == l.currentLine+2 && line >= 3 && l.previousImg != nil {
			// Skipped a line. It may happen and just copy over the previous image.
			// Do not copy over the telemetry line.
			log.Printf("skipped line %d (copying from previous buffer)", line)
			l.currentLine++
			off := line * 80
			copy(l.currentImg.Pix[off:off+80], l.previousImg.Pix[off:off+80])
		} else {
			log.Printf("expected line %d, got %d  %v", l.currentLine+1, line, l.packet)
			l.stats.Resets++
			l.spi.Reset()
			l.currentLine = -1
			return
		}
	}

	l.currentLine++
	//log.Printf("line: %d", l.currentLine)
	// Convert the line from byte to uint16. 14 bits significant.
	l.stats.GoodLines++
	if 3 <= line && line < 63 {
		line -= 3
		// Don't forget the 2 uint16 header (ID + CRC).
		for x := 0; x < 80; x++ {
			l.currentImg.Pix[line*80+x] = uint16(l.packet[2*x+4])<<8 | uint16(l.packet[2*x+5])
		}
	} else if line == 0 {
		// Telemetry line.
		copy(l.currentImg.Raw[:], l.packet[4:])
		if err := binary.Read(bytes.NewBuffer(l.currentImg.Raw[:]), binary.BigEndian, &l.currentImg.Telemetry); err != nil {
			fmt.Printf("\nFAILURE: %s\n", err)
		}
		rowA := &l.currentImg.Telemetry
		i := l.currentImg
		i.Revision = rowA.TelemetryRevision
		//i.SinceStartup = time.Duration(rowA.TimeCounter) * time.Millisecond
		//copy(i.DeviceSerial[:], rowA.ModuleSerial[:])
		//i.DeviceVersion = rowA.SoftwareRevision
		//i.FrameCount = rowA.FrameCounter
		i.Mean = rowA.FrameMean
		i.RawTemperature = rowA.FPATempCounts
		i.Temperature = rowA.FPATemp.ToC()
		i.RawTemperatureHousing = rowA.HousingTempCounts
		i.TemperatureHousing = rowA.HousingTemp.ToC()
		i.FFCTemperature = rowA.FPATempLastFFC.ToC()
		i.FFCSince = time.Duration(rowA.TimeCounterLastFFC) * time.Millisecond
		i.FFCTemperatureHousing = rowA.HousingTempLastFFC.ToC()
		i.FFCLog2 = rowA.Log2FFCFrames
		//if rowA.StatusBits&statusMask != 0 {
		//	fmt.Printf("\nMask: 0x%X Status: 0x%X\n", statusMask, rowA.StatusBits)
		//}
		//i.FFCDesired = rowA.StatusBits&statusFFCDesired != 0
		//i.AGCEnabled = rowA.StatusBits&statusAGCState != 0
		//i.Overtemp = rowA.StatusBits&statusOvertemp != 0
		// i.FFCState
		/*
			fccstate := (status & (1<<(31-5) + 1<<(31-4))) >> (31 - 4)
			if l.currentImg.Revision == 8 {
				switch fccstate {
				case 0:
					i.FFCState = FFCNever
				case 1:
					i.FFCState = FFCInProgress
				case 2:
					i.FFCState = FFCComplete
				default:
					panic(fmt.Errorf("unexpected fccstate %d; %v", fccstate, l.packet))
				}
			} else {
				switch fccstate {
				case 0:
					i.FFCState = FFCNever
				case 2:
					i.FFCState = FFCInProgress
				case 3:
					i.FFCState = FFCComplete
				default:
					//fmt.Fprintf(os.Stderr, "unexpected fccstate %d; %v", fccstate, l.packet)
				}
			}
		*/
	}
}

// As documented at p.19-20.
// '*' means the value observed in practice make sense.
// Value after '-' is observed value.
type TelemetryRowA struct {
	TelemetryRevision  uint16    // 0 *
	TimeCounter        uint32    // 1 Looks invalid - 49,34,0,148 - 86,48,0,164, doesn't make sense
	StatusBits         uint32    // 3 Bit field. Looks invalid - 8,8,0,0
	ModuleSerial       [16]uint8 // 5 Is empty (!)
	SoftwareRevision   uint64    // 13 Seems to be little endian - 229402106072596480
	Reserved17         uint16    // 17 - 1101
	Reserved18         uint16    // 18
	Reserved19         uint16    // 19
	FrameCounter       uint32    // 20
	FrameMean          uint16    // 22 * Maybe it's FrameCounter?
	FPATempCounts      uint16    // 23
	FPATemp            CentiK    // 24 *
	HousingTempCounts  uint16    // 25
	HousingTemp        CentiK    // 27 *
	Reserved27         uint16    // 27
	Reserved28         uint16    // 28
	FPATempLastFFC     CentiK    // 29
	TimeCounterLastFFC uint32    // 30
	HousingTempLastFFC CentiK    // 32
	Reserved33         uint16    // 33
	AGCROILeft         uint16    // 35 * - 0 (Likely inversed, haven't confirmed)
	AGCROITop          uint16    // 34 * - 0
	AGCROIRight        uint16    // 36 * - 79 - SDK was wrong!
	AGCROIBottom       uint16    // 37 * - 59 - SDK was wrong!
	AGCClipLimitHigh   uint16    // 38 *
	AGCClipLimitLow    uint16    // 39 *
	Reserved40         uint16    // 40 - 1
	Reserved41         uint16    // 41 - 128
	Reserved42         uint16    // 42 - 64
	Reserved43         uint16    // 43
	Reserved44         uint16    // 44
	Reserved45         uint16    // 45
	Reserved46         uint16    // 46
	Reserved47         uint16    // 47 - 1
	Reserved48         uint16    // 48 - 128
	Reserved49         uint16    // 49 - 1
	Reserved50         uint16    // 50
	Reserved51         uint16    // 51
	Reserved52         uint16    // 52
	Reserved53         uint16    // 53
	Reserved54         uint16    // 54
	Reserved55         uint16    // 55
	Reserved56         uint16    // 56 - 30
	Reserved57         uint16    // 57
	Reserved58         uint16    // 58 - 1
	Reserved59         uint16    // 59 - 1
	Reserved60         uint16    // 60 - 78
	Reserved61         uint16    // 61 - 58
	Reserved62         uint16    // 62 - 7
	Reserved63         uint16    // 63 - 90
	Reserved64         uint16    // 64 - 40
	Reserved65         uint16    // 65 - 210
	Reserved66         uint16    // 66 - 255
	Reserved67         uint16    // 67 - 255
	Reserved68         uint16    // 68 - 23
	Reserved69         uint16    // 69 - 6
	Reserved70         uint16    // 70
	Reserved71         uint16    // 71
	Reserved72         uint16    // 72 - 7
	Reserved73         uint16    // 73
	Log2FFCFrames      uint16    // 74 Found 3, should be 27?
	Reserved75         uint16    // 75
	Reserved76         uint16    // 76
	Reserved77         uint16    // 77
	Reserved78         uint16    // 78
	Reserved79         uint16    // 79
}

// As documented as page.21
const (
	statusFFCDesired uint32 = 1 << (31 - 3)
	statusFFCState   uint32 = 1<<(31-4) | 1<<(31-5)
	statusAGCState   uint32 = 1 << (31 - 12)
	statusOvertemp   uint32 = 1 << (31 - 20)
	statusMask              = ^(statusFFCDesired | statusFFCState | statusAGCState | statusOvertemp)
)
