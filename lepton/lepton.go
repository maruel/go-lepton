// Copyright 2015 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

// Package lepton takes video from FLIR Lepton connected to a Raspberry Pi SPI
// port.
//
// References
//
// Official FLIR reference:
//   http://www.flir.com/cvs/cores/view/?id=51878
//
// Product page:
//   http://www.flir.com/cores/content/?id=66257
//
// Datasheet:
//   http://www.flir.com/uploadedFiles/OEM/Products/LWIR-Cameras/Lepton/Lepton%20Engineering%20Datasheet%20-%20with%20Radiometry.pdf
//
//
// TODO(maruel): Move the web site:
//
// The recommended buy is the Lepton breakout board at 239$USD at:
// https://store.groupgets.com/products/flir-lepton-breakout-board-with-radiometric-flir-lepton-2-5
// Note that this driver was tested with an older version of this board.
//
// DIY:
//   http://www.pureengineering.com/projects/lepton
//
// DYI help group mailing list:
//   https://groups.google.com/d/forum/flir-lepton
package lepton

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"log"
	"sync"
	"time"

	"github.com/maruel/go-lepton/lepton/cci"
	"github.com/maruel/go-lepton/lepton/internal"
	"periph.io/x/periph/conn"
	"periph.io/x/periph/conn/gpio"
	"periph.io/x/periph/conn/i2c"
	"periph.io/x/periph/conn/spi"
	"periph.io/x/periph/devices"
)

// Metadata is constructed from TelemetryRowA, which is sent at each frame.
type Metadata struct {
	SinceStartup   time.Duration   //
	FrameCount     uint32          // Number of frames since the start of the camera, in 27fps (not 9fps).
	AvgValue       uint16          // Average value of the buffer.
	Temp           devices.Celsius // Temperature inside the camera.
	TempHousing    devices.Celsius // Temperature of the housing of the camera.
	RawTemp        uint16          //
	RawTempHousing uint16          //
	FFCSince       time.Duration   // Time since last internal calibration.
	FFCTemp        devices.Celsius // Temperature at last internal calibration.
	FFCTempHousing devices.Celsius //
	FFCState       cci.FFCState    // Current calibration state.
	FFCDesired     bool            // Asserted at start-up, after period (default 3m) or after temperature change (default 3°K). Indicates that a calibration should be triggered as soon as possible.
	Overtemp       bool            // true 10s before self-shutdown.
}

// Frame is a FLIR Lepton frame, containing 14 bits resolution intensity stored
// as image.Gray16.
//
// Values centered around 8192 accorging to camera body temperature. Effective
// range is 14 bits, so [0, 16383].
//
// Each 1 increment is approximatively 0.025°K.
type Frame struct {
	*image.Gray16
	Metadata Metadata // Metadata that is sent along the pixels.
}

// Stats is internal statistics about the frame grabbing.
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

// Dev controls a FLIR Lepton.
//
// It assumes a specific breakout board. Sadly the breakout board doesn't
// expose the PWR_DWN_L and RESET_L lines so it is impossible to shut down the
// Lepton.
type Dev struct {
	*cci.Dev
	s              spi.Conn
	cs             gpio.PinOut
	prevImg        *image.Gray16
	frameA, frameB []byte
	frameWidth     int // in bytes
	frameLines     int
	maxTxSize      int
	stats          Stats
}

// New returns an initialized connection to the FLIR Lepton.
//
// Maximum SPI speed is 20Mhz. Minimum usable rate is ~2.2Mhz to
// sustain 9hz framerate at 80x60.
//
// Maximum I²C speed is 1Mhz.
//
// MOSI is not used and should be grounded.
func New(s spi.Conn, i i2c.Bus) (*Dev, error) {
	// Sadly the Lepton will inconditionally send 27fps, even if the effective
	// rate is 9fps.
	// Query the CS pin before disabling it.
	p, ok := s.(spi.Pins)
	if !ok {
		return nil, errors.New("lepton: require manual access to the CS pin")
	}
	cs := p.CS()
	if cs == gpio.INVALID {
		return nil, errors.New("lepton: require manual access to a valid CS pin")
	}
	if err := s.DevParams(20000000, spi.Mode3|spi.NoCS, 8); err != nil {
		return nil, err
	}
	c, err := cci.New(i)
	if err != nil {
		return nil, err
	}
	// TODO(maruel): Support Lepton 3 with 160x120.
	w := 80
	h := 60
	// telemetry data is a 3 lines header.
	frameLines := h + 3
	frameWidth := w*2 + 4
	d := &Dev{
		Dev:        c,
		s:          s,
		cs:         cs,
		prevImg:    image.NewGray16(image.Rect(0, 0, w, h)),
		frameWidth: frameWidth,
		frameLines: frameLines,
	}
	if l, ok := s.(conn.Limits); ok {
		d.maxTxSize = l.MaxTxSize()
	}
	if status, err := d.GetStatus(); err != nil {
		return nil, err
	} else if status.CameraStatus != cci.SystemReady {
		// The lepton takes < 1 second to boot so it should not happen normally.
		return nil, fmt.Errorf("lepton: camera is not ready: %s", status)
	}
	if err := d.Init(); err != nil {
		return nil, err
	}
	return d, nil
}

func (d *Dev) Stats() Stats {
	// TODO(maruel): atomic.
	return d.stats
}

// ReadImg reads an image.
//
// It is ok to call other functions concurrently to send commands to the
// camera.
func (d *Dev) ReadImg() (*Frame, error) {
	f := &Frame{Gray16: image.NewGray16(d.prevImg.Bounds())}
	for {
		if err := d.readFrame(f); err != nil {
			return nil, err
		}
		if f.Metadata.FFCDesired == true {
			// TODO(maruel): Automatically trigger FFC when applicable.
			// TODO(maruel): Determine if the camera has a shutter.
			//go d.RunFFC()
			//return nil, errors.New("FFC is desired")
		}
		if !bytes.Equal(d.prevImg.Pix, f.Gray16.Pix) {
			d.stats.GoodFrames++
			break
		}
		// It also happen if the image is 100% static without noise.
		d.stats.DuplicateFrames++
	}
	copy(d.prevImg.Pix, f.Pix)
	return f, nil
}

// Private details.

// stream reads continuously from the SPI connection.
func (d *Dev) stream(done <-chan struct{}, c chan<- []byte) error {
	lines := d.frameLines * 2
	if d.maxTxSize != 0 {
		if l := d.maxTxSize / d.frameWidth; l < lines {
			lines = l
		}
	}
	if err := d.cs.Out(gpio.Low); err != nil {
		return err
	}
	defer d.cs.Out(gpio.High)
	for {
		// TODO(maruel): Use a ring buffer to stop continuously allocating.
		buf := make([]byte, d.frameWidth*lines)
		if err := d.s.Tx(nil, buf); err != nil {
			return err
		}
		for i := 0; i < len(buf); i += d.frameWidth {
			select {
			case <-done:
				return nil
			case c <- buf[i : i+d.frameWidth]:
			}
		}
	}
	/*
		LastFail        error
		Resets          int
		GoodFrames      int
		DuplicateFrames int
		TransferFails   int
		GoodLines       int
		BrokenLines     int
		DiscardLines    int
		BadSyncLines    int
	*/
}

// readFrame reads one frame.
//
// Each frame is sent as a packet over SPI including telemetry data as an
// header. See page 49-57 for "VoSPI" protocol explanation.
//
// This operation must complete within 32ms. Frames occur every 38.4ms at
// almost 27hz.
//
// Resynchronization is done by deasserting CS and CLK for at least 5 frames
// (>185ms).
//
// When a packet starts, it must be completely clocked out within 3 line
// periods.
//
// One frame of 80x60 at 2 byte per pixel, plus 4 bytes overhead per line plus
// 3 lines of telemetry is (3+60)*(4+160) = 10332. The sysfs-spi driver limits
// each transaction size, the default is 4Kb. To reduce the risks of failure,
// reads 4Kb at a time and figure out the lines from there. The Lepton is very
// cranky if reading is not done quickly enough.
func (d *Dev) readFrame(f *Frame) error {
	done := make(chan struct{})
	c := make(chan []byte, 1024)
	var err error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		err = d.stream(done, c)
	}()
	defer func() {
		done <- struct{}{}
	}()

	max := 200 * time.Millisecond
	timeout := time.After(max)
	w := f.Bounds().Dx()
	sync := 0
	discard := 0
	for {
		select {
		case <-timeout:
			return fmt.Errorf("failed to synchronize after %s", max)
		case l, ok := <-c:
			if !ok {
				wg.Wait()
				return err
			}
			h := internal.Big16.Uint16(l)
			if h&packetHeaderDiscard == packetHeaderDiscard {
				discard++
				sync = 0
				continue
			}
			headerID := h & packetHeaderMask
			if discard != 0 {
				//log.Printf("discarded %d", discard)
				discard = 0
				sync = 0
			}
			if false {
				// Having trouble.
				if int(headerID) == 0 && sync == 0 && !verifyCRC(l) {
					log.Printf("no crc")
					sync = 0
					continue
				}
			}
			if int(headerID) != sync {
				log.Printf("%d != %d", headerID, sync)
				sync = 0
				continue
			}
			if sync == 0 {
				// Parse the first row of telemetry data.
				if err2 := f.Metadata.parseTelemetry(l[4:]); err2 != nil {
					log.Printf("OMG: %v", err2)
					continue
				}
			} else if sync == d.frameLines-1 {
				return nil
			} else if sync >= 3 {
				// Image.
				for x := 0; x < w; x++ {
					o := 4 + x*2
					f.SetGray16(x, sync-3, color.Gray16{internal.Big16.Uint16(l[o : o+2])})
				}
			}
			sync++
		}
	}
	return nil
}

func (m *Metadata) parseTelemetry(data []byte) error {
	// Telemetry line.
	var rowA internal.TelemetryRowA
	if err := binary.Read(bytes.NewBuffer(data), internal.Big16, &rowA); err != nil {
		return err
	}
	m.SinceStartup = rowA.TimeCounter.ToD()
	m.FrameCount = rowA.FrameCounter
	m.AvgValue = rowA.FrameMean
	m.Temp = rowA.FPATemp.ToC()
	m.TempHousing = rowA.HousingTemp.ToC()
	m.RawTemp = rowA.FPATempCounts
	m.RawTempHousing = rowA.HousingTempCounts
	m.FFCSince = rowA.TimeCounterLastFFC.ToD()
	m.FFCTemp = rowA.FPATempLastFFC.ToC()
	m.FFCTempHousing = rowA.HousingTempLastFFC.ToC()
	if rowA.StatusBits&statusMaskNil != 0 {
		return fmt.Errorf("\n(Status: 0x%08X) & (Mask: 0x%08X) = (Extra: 0x%08X) in 0x%08X\n", rowA.StatusBits, statusMask, rowA.StatusBits&statusMaskNil, statusMaskNil)
	}
	m.FFCDesired = rowA.StatusBits&statusFFCDesired != 0
	m.Overtemp = rowA.StatusBits&statusOvertemp != 0
	fccstate := rowA.StatusBits & statusFFCStateMask >> statusFFCStateShift
	if rowA.TelemetryRevision == 8 {
		switch fccstate {
		case 0:
			m.FFCState = cci.FFCNever
		case 1:
			m.FFCState = cci.FFCInProgress
		case 2:
			m.FFCState = cci.FFCComplete
		default:
			return fmt.Errorf("unexpected fccstate %d; %v", fccstate, data)
		}
	} else {
		switch fccstate {
		case 0:
			m.FFCState = cci.FFCNever
		case 2:
			m.FFCState = cci.FFCInProgress
		case 3:
			m.FFCState = cci.FFCComplete
		default:
			return fmt.Errorf("unexpected fccstate %d; %v", fccstate, data)
		}
	}
	return nil
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

// verifyCRC test the equation x^16 + x^12 + x^5 + x^0
func verifyCRC(d []byte) bool {
	tmp := make([]byte, len(d))
	copy(tmp, d)
	tmp[0] &^= 0x0F
	tmp[2] = 0
	tmp[3] = 0
	crc := internal.CRC16(tmp)
	actual := internal.Big16.Uint16(d[2:])
	return crc == actual
}
