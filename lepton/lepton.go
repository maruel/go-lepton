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

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"time"
)

// lepton controls a FLIR Lepton. It assumes a specific breakout board. Sadly
// the breakout board doesn't expose the PWR_DWN_L and RESET_L lines so it is
// impossible to shut down the Lepton.
type lepton struct {
	spi               *SPI
	i2c               *I2C
	currentImg        *LeptonBuffer
	previousImg       *LeptonBuffer
	lastLine          int        // Last valid line number, or -1 if no valid line was yet received.
	packet            [164]uint8 // one line is sent as a SPI packet.
	stats             Stats
	serial            uint64
	telemetry         Flag
	telemetryLocation TelemetryLocation
}

func MakeLepton(path string, speed int) (Lepton, error) {
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
	if speed > 25000000 {
		return nil, errors.New("speed specified is too high")
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
	i2c, err := MakeI2CLepton()
	defer func() {
		if i2c != nil {
			i2c.Close()
		}
	}()
	if err != nil {
		return nil, err
	}

	// Send a ping to ensure the device is working.
	out := &lepton{spi: spi, i2c: i2c, lastLine: -1, telemetry: Enabled, telemetryLocation: Header}
	status, err := out.GetStatus()
	if err != nil {
		return nil, err
	}
	if status.CameraStatus != SystemReady {
		log.Printf("WARNING: camera is not ready: %s", status)
	}

	agc := Disabled
	if err := i2c.GetAttribute(AgcEnable, &agc); err != nil {
		return nil, err
	}
	if agc != Disabled {
		log.Printf("WARNING: AGC is %s", agc)
		if err := i2c.SetAttribute(AgcEnable, Disabled); err != nil {
			return nil, err
		}
	}
	// Setup telemetry.
	if err := i2c.SetAttribute(SysTelemetryEnable, out.telemetry); err != nil {
		return nil, err
	}
	// I had trouble using it as footer.
	if err := i2c.SetAttribute(SysTelemetryLocation, out.telemetryLocation); err != nil {
		return nil, err
	}

	spi = nil
	i2c = nil
	return out, nil
}

func (l *lepton) Close() error {
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

func (l *lepton) GetStatus() (*Status, error) {
	out := &Status{}
	return out, l.i2c.GetAttribute(SysStatus, out)
}

func (l *lepton) GetSerial() (uint64, error) {
	if l.serial == 0 {
		out := uint64(0)
		if err := l.i2c.GetAttribute(SysSerialNumber, &out); err != nil {
			return out, err
		}
		l.serial = out
	}
	return l.serial, nil
}

func (l *lepton) GetUptime() (time.Duration, error) {
	var out DurationMS
	err := l.i2c.GetAttribute(SysUptime, &out)
	return out.ToDuration(), err
}

func (l *lepton) GetTemperatureHousing() (CentiC, error) {
	var out CentiK
	err := l.i2c.GetAttribute(SysHousingTemperature, &out)
	return out.ToC(), err
}

func (l *lepton) GetTemperature() (CentiC, error) {
	var out CentiK
	err := l.i2c.GetAttribute(SysTemperature, &out)
	return out.ToC(), err
}

func (l *lepton) GetFFCModeControl() (*FFCMode, error) {
	out := &FFCMode{}
	return out, l.i2c.GetAttribute(SysFFCMode, out)
}

func (l *lepton) GetShutterPosition() (ShutterPosition, error) {
	var out ShutterPosition
	err := l.i2c.GetAttribute(SysShutterPosition, &out)
	return out, err
}

func (l *lepton) GetTelemetryEnable() (Flag, error) {
	var out Flag
	err := l.i2c.GetAttribute(SysTelemetryEnable, &out)
	return out, err
}

func (l *lepton) GetTelemetryLocation() (TelemetryLocation, error) {
	var out TelemetryLocation
	err := l.i2c.GetAttribute(SysTelemetryLocation, &out)
	return out, err
}

func (l *lepton) TriggerFFC() error {
	return l.i2c.RunCommand(SysFCCRunNormalization)
}

func (l *lepton) Stats() Stats {
	// TODO(maruel): atomic.
	return l.stats
}

func (l *lepton) ReadImg() *LeptonBuffer {
	l.lastLine = -1
	l.previousImg = l.currentImg
	l.currentImg = &LeptonBuffer{}
	for {
		// TODO(maruel): Fail after N errors?
		// TODO(maruel): Skip 2 frames since they'll be the same data so no need
		// for the check below.
		for l.lastLine != l.maxLine() {
			l.readLine()
		}
		// Automatically trigger FFC when applicable.
		// TODO(maruel): Determine if the camera has a shutter.
		if l.currentImg.Metadata.FFCDesired == true {
			//go l.TriggerFFC()
		}
		if l.previousImg == nil || !l.previousImg.Equal(l.currentImg) {
			l.stats.GoodFrames++
			break
		}
		// It also happen if the image is 100% static without noise.
		l.stats.DuplicateFrames++
		l.lastLine = -1
	}
	return l.currentImg
}

// Private details.

// maxLine returns the last valid VoSPI line. Returns 59 or 62.
func (l *lepton) maxLine() int {
	if l.telemetry != Disabled {
		return 59 + 3
	}
	return 59
}

// realLine returns the image or telemetry line.
func (l *lepton) realLine(line int) (imgLine int, telemetryLine int) {
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
func (l *lepton) readLine() {
	// Operation must complete within 32ms. Frames occur every 38.4ms. With SPI,
	// write must occur as read is being done, just sent dummy data.
	n, err := l.spi.Read(l.packet[:])
	if n != len(l.packet) && err == nil {
		err = fmt.Errorf("unexpected read %d", n)
	}
	if err != nil {
		l.stats.TransferFails++
		l.lastLine = -1
		if l.stats.LastFail == nil {
			log.Printf("I/O fail: %s", err)
			l.stats.LastFail = err
		}
		l.stats.Resets++
		l.spi.Reset()
		return
	}

	l.stats.LastFail = nil
	headerLine := int(binary.BigEndian.Uint16(l.packet[:2])) & packetHeaderMask
	if (headerLine & packetHeaderDiscard) == packetHeaderDiscard {
		// Discard packet. This happens as the bandwidth of SPI is larger than data
		// rate.
		//l.lastLine = -1
		l.stats.DiscardLines++
		return
	}

	if headerLine > l.maxLine() {
		log.Printf("got unexpected line %d  %v", headerLine, l.packet)
		l.stats.Resets++
		l.spi.Reset()
		l.stats.BrokenLines++
		l.lastLine = -1
		return
	}

	// TODO(maruel): Do CRC check.

	imgLine, telemetryLine := l.realLine(headerLine)
	if headerLine != l.lastLine+1 {
		l.stats.BadSyncLines++
		if headerLine == l.lastLine {
			// That's bad and shouldn't (?) happen.
			log.Printf("duplicate line %d\n  %v", headerLine, l.packet[:8])
			return
		}
		if headerLine == 0 {
			// A new frame was started, ignore the previous one.
			log.Printf("reset")
			l.lastLine = -1
			/*
				} else if headerLine == l.lastLine+2 && headerLine >= 3 && l.previousImg != nil {
					// Skipped a line. It may happen and just copy over the previous image.
					// Do not copy over the telemetry line.
					log.Printf("skipped line %d (copying from previous buffer)", headerLine)
					l.lastLine++
					off := headerLine * 80
					copy(l.currentImg.Pix[off:off+80], l.previousImg.Pix[off:off+80])
			*/
		} else {
			log.Printf("expected line %d, got %d\n  %v", l.lastLine+1, headerLine, l.packet[:8])
			l.stats.Resets++
			l.spi.Reset()
			l.lastLine = -1
			return
		}
	}

	l.lastLine++
	//log.Printf("line: %d", l.lastLine)
	// Convert the line from byte to uint16. 14 bits significant.
	l.stats.GoodLines++
	if imgLine != -1 {
		// Skip 2 uint16 header (ID + CRC). Line number is offset by 3.
		// Can't use this due to type difference:
		//   copy(l.currentImg.Pix[(imgLine-3)*80:], l.packet[4:])
		// I think that the following would be slower, needs to be tested:
		//   binary.Read(bytes.NewBuffer(l.packet[4:]), binary.BigEndian, l.currentImg.Pix[base:])
		base := imgLine * 80
		for x := 0; x < 80; x++ {
			l.currentImg.Pix[base+x] = binary.BigEndian.Uint16(l.packet[2*x+4:])
		}
	} else if telemetryLine != -1 {
		l.parseTelemetry(telemetryLine)
	} else {
		panic("internal error")
	}
}

func (l *lepton) parseTelemetry(line int) {
	if line > 0 {
		for i := 4; i < len(l.packet); i++ {
			if l.packet[i] != 0 {
				//log.Printf("got unexpected telemetry line %d  %v", headerLine, l.packet)
				l.stats.BrokenLines++
				break
			}
		}
		return
	}
	// Telemetry line. Swap endian here since it's not swapped in SPI.Read().
	telemetry := l.packet[4:]
	uint16Swap(telemetry)
	if err := binary.Read(bytes.NewBuffer(telemetry), binary.LittleEndian, &l.currentImg.Telemetry); err != nil {
		fmt.Printf("\nFAILURE: %s\n", err)
	}
	rowA := &l.currentImg.Telemetry
	i := &l.currentImg.Metadata
	i.SinceStartup = rowA.TimeCounter.ToDuration()
	i.FrameCount = rowA.FrameCounter
	i.AverageValue = rowA.FrameMean
	i.Temperature = rowA.FPATemp.ToC()
	i.TemperatureHousing = rowA.HousingTemp.ToC()
	i.RawTemperature = rowA.FPATempCounts
	i.RawTemperatureHousing = rowA.HousingTempCounts
	i.FFCSince = rowA.TimeCounterLastFFC.ToDuration()
	i.FFCTemperature = rowA.FPATempLastFFC.ToC()
	i.FFCTemperatureHousing = rowA.HousingTempLastFFC.ToC()
	if rowA.StatusBits&statusMaskNil != 0 {
		fmt.Printf("\n(Status: 0x%08X) & (Mask: 0x%08X) = (Extra: 0x%08X) in 0x%08X\n", rowA.StatusBits, statusMask, rowA.StatusBits&statusMaskNil, statusMaskNil)
	}
	i.FFCDesired = rowA.StatusBits&statusFFCDesired != 0
	i.Overtemp = rowA.StatusBits&statusOvertemp != 0
	fccstate := rowA.StatusBits & statusFFCStateMask >> statusFFCStateShift
	if rowA.TelemetryRevision == 8 {
		switch fccstate {
		case 0:
			i.FFCState = FFCNever
		case 1:
			i.FFCState = FFCInProgress
		case 2:
			i.FFCState = FFCComplete
		default:
			log.Printf("unexpected fccstate %d; %v", fccstate, l.packet)
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
			log.Printf("unexpected fccstate %d; %v", fccstate, l.packet)
		}
	}
}

// As documented at p.19-20.
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

// As documented as page.21
const (
	packetHeaderDiscard = 0x0F00
	packetHeaderMask    = 0x0FFF // ID field is 12 bits. Leading 4 bits are reserved.
	// Observed status:
	//   0x00000808
	//   0x00007A01
	//   0x00022200
	//   0x01AD0000
	//   0x02BF0000
	//   0x1FFF0000
	//   0x3FFF0001
	//   0xDCD0FFFF
	//   0xFFDCFFFF
	statusFFCDesired    uint32 = 1 << 3                                                                                   // 0x00000008
	statusFFCStateMask  uint32 = 1<<4 | 1<<5                                                                              // 0x00000030
	statusFFCStateShift uint32 = 4                                                                                        //
	statusReserved      uint32 = 1 << 11                                                                                  // 0x00000800
	statusAGCState      uint32 = 1 << 12                                                                                  // 0x00001000
	statusOvertemp      uint32 = 1 << 20                                                                                  // 0x00100000
	statusMask                 = statusFFCDesired | statusFFCStateMask | statusAGCState | statusOvertemp | statusReserved // 0x00101838
	statusMaskNil              = ^statusMask                                                                              // 0xFFEFE7C7
)
