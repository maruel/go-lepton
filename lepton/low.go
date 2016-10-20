// Copyright 2015 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package lepton

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

// SPI is the Lepton specific VoSPI interface.
//
// It's essentially little endian encoded stream over big endian 16 bits words.
// #thanksobama.
type SPI struct {
	closed int32
	path   string
	speed  int
	lock   sync.Mutex
	f      *os.File
}

func MakeSPI(path string, speed int) (*SPI, error) {
	if path == "" {
		path = "/dev/spidev0.0"
	}
	f, err := os.OpenFile(path, os.O_RDWR, os.ModeExclusive)
	if err != nil {
		return nil, err
	}
	s := &SPI{path: path, speed: speed, f: f}
	if err := s.setFlag(spiIOCMode, 3); err != nil {
		s.Close()
		return nil, err
	}
	if err := s.setFlag(spiIOCBitsPerWord, 8); err != nil {
		s.Close()
		return nil, err
	}
	if err := s.setFlag(spiIOCMaxSpeedHz, uint64(speed)); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

func (s *SPI) Close() error {
	if !atomic.CompareAndSwapInt32(&s.closed, 0, 1) {
		return io.ErrClosedPipe
	}
	s.lock.Lock()
	defer s.lock.Unlock()
	var err error
	if s.f != nil {
		err = s.f.Close()
		s.f = nil
	}
	return err
}

func (s *SPI) Reset() error {
	if atomic.LoadInt32(&s.closed) != 0 {
		return io.ErrClosedPipe
	}
	s.lock.Lock()
	defer s.lock.Unlock()
	log.Printf("SPI.Reset()")
	// If out of sync, Deassert /CS and idle SCK for at least 5 frame periods
	// (>185ms).
	/*
		s.f.Close()
		time.Sleep(200 * time.Millisecond)
		tmp, err := MakeSPI(s.path, s.speed)
		if err != nil {
			return err
		}
		s.f = tmp.f
		return err
	*/
	time.Sleep(200 * time.Millisecond)
	return nil
}

// Read returns the data as 16bits big endian words as described in VoSPI
// protocol. Always return an error if the whole buffer wasn't read.
//
// The reason it doesn't return as little endian is to save on CPU processing
// as the vast majority of lines are 'discard' lines that are not processed in
// any way.
func (s *SPI) Read(b []byte) (int, error) {
	if atomic.LoadInt32(&s.closed) != 0 {
		return 0, io.ErrClosedPipe
	}
	s.lock.Lock()
	defer s.lock.Unlock()
	n, err := s.f.Read(b)
	if err == nil && n != len(b) {
		err = io.ErrShortBuffer
	}
	return n, err
}

// Private details.

// spidev driver IOCTL control codes.
const (
	spiIOCMode        = 0x16B01
	spiIOCBitsPerWord = 0x16B03
	spiIOCMaxSpeedHz  = 0x46B04
)

func (s *SPI) setFlag(op uint, arg uint64) error {
	if atomic.LoadInt32(&s.closed) != 0 {
		return io.ErrClosedPipe
	}
	s.lock.Lock()
	defer s.lock.Unlock()
	if err := s.ioctl(op|0x40000000, unsafe.Pointer(&arg)); err != nil {
		return err
	}
	actual := uint64(0)
	// getFlag() equivalent.
	if err := s.ioctl(op|0x80000000, unsafe.Pointer(&actual)); err != nil {
		return err
	}
	if actual != arg {
		return fmt.Errorf("spi op 0x%x: set 0x%x, read 0x%x", op, arg, actual)
	}
	return nil
}

func (s *SPI) ioctl(op uint, arg unsafe.Pointer) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, s.f.Fd(), uintptr(op), uintptr(arg)); errno != 0 {
		return fmt.Errorf("spi ioctl: %s", syscall.Errno(errno))
	}
	return nil
}

///

// I2C is the Lepton specific Command and Control Interface (CCI).
//
// It's essentially little endian encoded stream over big endian 16 bits words.
// #thanksobama.
type I2C struct {
	closed int32
	lock   sync.Mutex
	f      *os.File
}

func MakeI2C() (*I2C, error) {
	path := fmt.Sprintf("/dev/i2c-%v", 1)
	f, err := os.OpenFile(path, os.O_RDWR, os.ModeExclusive)
	if err != nil {
		return nil, err
	}
	return &I2C{f: f}, nil
}

func (i *I2C) Close() error {
	if !atomic.CompareAndSwapInt32(&i.closed, 0, 1) {
		return io.ErrClosedPipe
	}
	i.lock.Lock()
	defer i.lock.Unlock()
	var err error
	if i.f != nil {
		err = i.f.Close()
		i.f = nil
	}
	return err
}

// Private details.

const (
	i2cIOCSetAddress = 0x0703 // i2c-dev IOCTL control code I2C_SLAVE
)

// read converts the 16bits big endian words into litte endian on the fly. Will
// always return an error if the whole buffer wasn't read.
func (i *I2C) read(b []byte) (int, error) {
	if len(b)&1 != 0 {
		panic("lepton CCI requires 16 bits aligned read")
	}
	n, err := i.f.Read(b)
	uint16Swap(b[:n])
	if err == nil && n != len(b) {
		err = io.ErrShortBuffer
	}
	return n, err
}

// write takes little endian data and writes it as bid endian 16bit words.
func (i *I2C) write(b []byte) (int, error) {
	if len(b)&1 != 0 {
		panic("lepton CCI requires 16 bits aligned write")
	}
	// Create a temporary slice to conform to io.Writer (even if this function is
	// not exported).
	tmp := make([]byte, len(b))
	copy(tmp, b)
	uint16Swap(tmp)
	n, err := i.f.Write(tmp)
	return n, err
}

func (i *I2C) ioctl(op uint, arg uintptr) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, i.f.Fd(), uintptr(op), arg); errno != 0 {
		return fmt.Errorf("i2c ioctl: %s", syscall.Errno(errno))
	}
	return nil
}

func (i *I2C) readRegister(addr RegisterAddress) (uint16, error) {
	data := []byte{0, 0}
	err := i.readData(addr, data)
	return binary.LittleEndian.Uint16(data), err
}

func (i *I2C) readData(addr RegisterAddress, data []byte) error {
	if atomic.LoadInt32(&i.closed) != 0 {
		return io.ErrClosedPipe
	}
	if _, err := i.write(putUint16(uint16(addr))); err != nil {
		return err
	}
	_, err := i.read(data)
	return err
}

func (i *I2C) writeRegister(addr RegisterAddress, data uint16) error {
	return i.writeData(addr, putUint16(data))
}

func (i *I2C) writeData(addr RegisterAddress, data []byte) error {
	if atomic.LoadInt32(&i.closed) != 0 {
		return io.ErrClosedPipe
	}
	tmp := make([]byte, 0, len(data)+2)
	tmp = append(tmp, putUint16(uint16(addr))...)
	tmp = append(tmp, data...)
	_, err := i.write(tmp)
	return err
}

// putUint16 encodes as little endian.
func putUint16(v uint16) []byte {
	p := make([]byte, 2)
	binary.LittleEndian.PutUint16(p, v)
	return p
}

// Swaps little endian byte stream as big endian 16bit words. This is so fucked
// up, someone at FLIR is smoking crack.
func uint16Swap(p []byte) {
	if len(p)&1 != 0 {
		panic("bad length")
	}
	for i := 0; i < len(p)/2; i++ {
		j := 2 * i
		k := j + 1
		p[j], p[k] = p[k], p[j]
	}
}
