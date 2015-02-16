// Copyright 2015 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

// Do not use embd because its SPI and i²c implementations are too slow.

package lepton

import (
	"fmt"
	"io"
	"os"
	"syscall"
	"unsafe"
)

type SPI struct {
	f *os.File
}

func MakeSPI(path string, speed int) (*SPI, error) {
	if path == "" {
		path = "/dev/spidev0.0"
	}
	f, err := os.OpenFile(path, os.O_RDWR, os.ModeExclusive)
	if err != nil {
		return nil, err
	}
	s := &SPI{f: f}
	if err := s.SetFlag(spiIOCMode, 3); err != nil {
		return s, err
	}
	if err := s.SetFlag(spiIOCBitsPerWord, 8); err != nil {
		return s, err
	}
	if err := s.SetFlag(spiIOCMaxSpeedHz, uint64(speed)); err != nil {
		return s, err
	}
	return s, nil
}

func (s *SPI) Close() error {
	var err error
	if s.f != nil {
		err = s.f.Close()
		s.f = nil
	}
	return err
}

func (s *SPI) GetFlag(op uint, arg *uint64) error {
	return s.ioctl(op|0x80000000, unsafe.Pointer(arg))
}

func (s *SPI) SetFlag(op uint, arg uint64) error {
	if err := s.ioctl(op|0x40000000, unsafe.Pointer(&arg)); err != nil {
		return err
	}
	actual := uint64(0)
	if err := s.GetFlag(op, &actual); err != nil {
		return err
	}
	if actual != arg {
		return fmt.Errorf("spi op 0x%x: set 0x%x, read 0x%x", op, arg, actual)
	}
	return nil
}

func (s *SPI) Read(b []byte) (int, error) {
	return s.f.Read(b)
}

func (s *SPI) ioctl(op uint, arg unsafe.Pointer) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, s.f.Fd(), uintptr(op), uintptr(arg)); errno != 0 {
		return fmt.Errorf("spi ioctl: %s", syscall.Errno(errno))
	}
	return nil
}

///

type I2C struct {
	f *os.File
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
	var err error
	if i.f != nil {
		err = i.f.Close()
		i.f = nil
	}
	return err
}

func (i *I2C) Read(b []byte) (int, error) {
	return i.f.Read(b)
}

func (i *I2C) Write(b []byte) (int, error) {
	return i.f.Write(b)
}

func (i *I2C) Cmd(cmdID int, data []byte, result []byte) error {
	cmdWord := make([]byte, 2, len(data)+2)
	cmdWord[0] = byte(cmdID >> 8)
	cmdWord[1] = byte(cmdID & 0xff)
	cmdWord = append(cmdWord, data...)
	if _, err := i.Write(cmdWord); err != nil {
		return fmt.Errorf("i2c write fail: %s", err)
	}
	if len(result) != 0 {
		n, err := i.Read(result)
		if n != len(result) {
			return io.ErrShortBuffer
		}
		if err != nil {
			return fmt.Errorf("i2c read fail: %s", err)
		}
	}
	return nil
}

func (i *I2C) SetAddress(address byte) error {
	return i.set(i2cIOCSetAddress, uint64(address))
}

func (i *I2C) ioctl(op uint, arg unsafe.Pointer) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, i.f.Fd(), uintptr(op), uintptr(arg)); errno != 0 {
		return fmt.Errorf("i2c ioctl: %s", syscall.Errno(errno))
	}
	return nil
}

func (i *I2C) set(op uint, arg uint64) error {
	return i.ioctl(op, unsafe.Pointer(&arg))
}

// Private details.

// Drivers IOCTL control codes.
const (
	spiIOCMode        = 0x16B01
	spiIOCBitsPerWord = 0x16B03
	spiIOCMaxSpeedHz  = 0x46B04
	i2cIOCSetAddress  = 0x0703
)
