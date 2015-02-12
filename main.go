// Copyright 2015 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

// Takes video from FLIR Lepton connected to a Raspberry Pi SPI port.
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
package main

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"os"
	"runtime/pprof"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/maruel/interrupt"
)

type rawBuffer [60][80]uint16

// scale reduces the dynamic range of a 14 bits down to 8 bits very naively.
func (r *rawBuffer) scale(dst *image.Gray) {
	floor := r.minVal()
	delta := int(r.maxVal() - floor)
	for y := 0; y < 60; y++ {
		for x := 0; x < 80; x++ {
			v := (int(r[y][x] - floor)) * 255 / delta
			dst.Pix[dst.Stride*y+x] = uint8(v)
		}
	}
}

func (r *rawBuffer) maxVal() uint16 {
	out := uint16(0)
	for y := 0; y < 60; y++ {
		for x := 0; x < 80; x++ {
			if r[y][x] > out {
				out = r[y][x]
			}
		}
	}
	return out
}

func (r *rawBuffer) minVal() uint16 {
	out := uint16(0xffff)
	for y := 0; y < 60; y++ {
		for x := 0; x < 80; x++ {
			if r[y][x] < out {
				out = r[y][x]
			}
		}
	}
	return out
}

func (r *rawBuffer) eq(l *rawBuffer) bool {
	for y := 0; y < 60; y++ {
		for x := 0; x < 80; x++ {
			if r[y][x] != l[y][x] {
				return false
			}
		}
	}
	return true
}

type rawBufferRing struct {
	c chan *rawBuffer
}

func makeRawBufferRing() *rawBufferRing {
	return &rawBufferRing{c: make(chan *rawBuffer, 16)}
}

func (r *rawBufferRing) get() *rawBuffer {
	select {
	case b := <-r.c:
		return b
	default:
		return &rawBuffer{}
	}
}

func (r *rawBufferRing) done(b *rawBuffer) {
	if len(r.c) < 8 {
		r.c <- b
	}
}

type Lepton struct {
	f               *os.File
	currentImg      *rawBuffer
	currentLine     int
	packet          [164]uint8 // one line is sent as a SPI packet.
	goodFrames      int        // stats
	duplicateFrames int
	transferFails   int
	lastFail        error
	brokenPackets   int
	syncFailures    int
	dummyLines      int
}

const (
	spiIOCWrMode        = 0x40016B01
	spiIOCWrBitsPerWord = 0x40016B03
	spiIOCWrMaxSpeedHz  = 0x40046B04

	spiIOCRdMode        = 0x80016B01
	spiIOCRdBitsPerWord = 0x80016B03
	spiIOCRdMaxSpeedHz  = 0x80046B04
)

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
		l.transferFails++
		l.currentLine = -1
		if l.lastFail == nil {
			fmt.Fprintf(os.Stderr, "\nI/O fail: %s\n\n", err)
			l.lastFail = err
		}
		time.Sleep(200 * time.Millisecond)
		return
	}

	l.lastFail = nil
	if (l.packet[0] & 0xf) == 0x0f {
		// Discard packet. This happens as the bandwidth of SPI is larger than data
		// rate.
		l.currentLine = -1
		l.dummyLines++
		return
	}

	// If out of sync, Deassert /CS and idle SCK for at least 5 frame periods
	// (>185ms).

	// TODO(maruel): Verify CRC (bytes 2-3) ?
	line := int(l.packet[1])
	if line > 60 {
		time.Sleep(200 * time.Millisecond)
		l.brokenPackets++
		l.currentLine = -1
		return
	}
	if line != l.currentLine+1 {
		time.Sleep(200 * time.Millisecond)
		l.syncFailures++
		l.currentLine = -1
		return
	}

	// Convert the line from byte to uint16. 14 bits significant.
	l.currentLine++
	for i := 0; i < 80; i++ {
		l.currentImg[line][i] = (uint16(l.packet[2*i+4])<<8 | uint16(l.packet[2*i+5]))
	}
}

// ReadImg reads an image into currentImg.
func (l *Lepton) ReadImg(r *rawBuffer) {
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
		if prevImg == nil || !prevImg.eq(l.currentImg) {
			l.goodFrames++
			break
		}
		// It also happen if the image is static.
		l.duplicateFrames++
		l.currentLine = -1
	}
}

type doubleBuffer struct {
	lock        sync.Mutex
	frontBuffer *image.Gray
	backBuffer  *image.Gray
}

var currentImage doubleBuffer

func serveImg(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "image/png")
	currentImage.lock.Lock()
	png.Encode(w, currentImage.frontBuffer)
	currentImage.lock.Unlock()
}

func mainImpl() error {
	cpuprofile := flag.String("cpuprofile", "", "dump CPU profile in file")
	port := flag.Int("port", 8010, "http port to listen on")
	flag.Parse()

	if len(flag.Args()) != 0 {
		return fmt.Errorf("unexpected argument: %s", flag.Args())
	}

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			return err
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	interrupt.HandleCtrlC()

	l, err := MakeLepton()
	if l != nil {
		defer l.Close()
	}
	if err != nil {
		return err
	}

	c := make(chan *rawBuffer, 16)

	currentImage.frontBuffer = image.NewGray(image.Rect(0, 0, 80, 60))
	currentImage.backBuffer = image.NewGray(image.Rect(0, 0, 80, 60))
	ring := makeRawBufferRing()

	go func() {
		for {
			// The idea is to keep this loop busy to not lose sync on SPI.
			b := ring.get()
			l.ReadImg(b)
			c <- b
		}
	}()

	go func() {
		for {
			// Processing is done in a separate loop.
			img := <-c
			img.scale(currentImage.backBuffer)
			ring.done(img)
			currentImage.lock.Lock()
			currentImage.backBuffer, currentImage.frontBuffer = currentImage.frontBuffer, currentImage.backBuffer
			currentImage.lock.Unlock()
		}
	}()

	http.HandleFunc("/", serveImg)
	fmt.Printf("Listening on %d\n\n", *port)
	go http.ListenAndServe(fmt.Sprintf(":%d", *port), nil)

	for !interrupt.IsSet() {
		// TODO(maruel): load variables via atomic.
		fmt.Printf("%d frames %d duped %d dummy %d badsync %d broken %d fail\r", l.goodFrames, l.duplicateFrames, l.dummyLines, l.syncFailures, l.brokenPackets, l.transferFails)
		time.Sleep(time.Second)
	}
	fmt.Print("\n")
	return nil
}

func main() {
	if err := mainImpl(); err != nil {
		fmt.Fprintf(os.Stderr, "go-lepton: %s.\n", err)
		os.Exit(1)
	}
}
