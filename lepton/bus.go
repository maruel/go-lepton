// Copyright 2015 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

// Do not use embd because its SPI and i²c implementations are too slow.

package lepton

import (
	"errors"
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

// Command to be sent over i²c.
type Command uint16

// All the available commands.
const (
	AGCEnable                 Command = 0x0100 // 2   GET/SET
	AgcRoiSelect              Command = 0x0108 // 4   GET/SET
	AgcHistogramStats         Command = 0x010C // 4   GET
	AgcHeqDampFactor          Command = 0x0124 // 1   GET/SET
	AgcHeqClipLimitHigh       Command = 0x012C // 1   GET/SET
	AgcHeqClipLimitLow        Command = 0x0130 // 1   GET/SET
	AgcHeqEmptyCounts         Command = 0x013C // 1   GET/SET
	AgcHeqOutputScaleFactor   Command = 0x0144 // 2   GET/SET
	AgcCalculationEnable      Command = 0x0148 // 2   GET/SET
	SysPing                   Command = 0x0200 // 0   RUN
	SysStatus                 Command = 0x0204 // 4   GET
	SysSerialNumber           Command = 0x0208 // 4   GET
	SysUptime                 Command = 0x020C // 2   GET
	SysHousingTemperature     Command = 0x0210 // 1   GET
	SysTemperature            Command = 0x0214 // 1   GET
	SysTelemetryEnable        Command = 0x0218 // 2   GET/SET
	SysTelemetryLocation      Command = 0x021C // 2   GET/SET
	SysExecuteFrameAverage    Command = 0x0220 // 0   RUN     Undocumented but listed in SDK
	SysFlatFieldFrames        Command = 0x0224 // 2   GET/SET It's an enum, max is 128
	SysCustomSerialNumber     Command = 0x0228 // 16  GET     It's a string
	SysRoiSceneStats          Command = 0x022C // 4   GET
	SysRoiSceneSelect         Command = 0x0230 // 4   GET/SET
	SysThermalShutdownCount   Command = 0x0234 // 1   GET     Number of times it exceeded 80C
	SysShutterPosition        Command = 0x0238 // 2   GET/SET
	SysFFCMode                Command = 0x023C // 20  GET/SET Manual control
	SysFCCRunNormalization    Command = 0x0240 // 0   RUN
	SysFCCStatus              Command = 0x0244 // 2   GET
	VidColorLookupSelect      Command = 0x0304 // 2   GET/SET
	VidColorLookupTransfer    Command = 0x0308 // 512 GET/SET
	VidFocusCalculationEnable Command = 0x030C // 2   GET/SET
	VidFocusRoiSelect         Command = 0x0310 // 4   GET/SET
	VidFocusMetricThreshold   Command = 0x0314 // 2   GET/SET
	VidFocusMetricGet         Command = 0x0318 // 2   GET
	VidVideoFreezeEnable      Command = 0x0324 // 2   GET/SET
)

// RegisterAddress is a valid register that can be read or written to.
type RegisterAddress uint16

// All the available registers.
const (
	RegPower       RegisterAddress = 0
	RegStatus      RegisterAddress = 2
	RegCommandID   RegisterAddress = 4
	RegDataLength  RegisterAddress = 6
	RegData0       RegisterAddress = 8
	RegData1       RegisterAddress = 10
	RegData2       RegisterAddress = 12
	RegData3       RegisterAddress = 14
	RegData4       RegisterAddress = 16
	RegData5       RegisterAddress = 18
	RegData6       RegisterAddress = 20
	RegData7       RegisterAddress = 22
	RegData8       RegisterAddress = 24
	RegData9       RegisterAddress = 26
	RegData10      RegisterAddress = 28
	RegData11      RegisterAddress = 30
	RegData12      RegisterAddress = 32
	RegData13      RegisterAddress = 34
	RegData14      RegisterAddress = 36
	RegData15      RegisterAddress = 38
	RegDataCRC     RegisterAddress = 40
	RegDataBuffer0 RegisterAddress = 0xF800
	RegDataBuffer1 RegisterAddress = 0xFC00
)

// RegStatus bitmask.
const (
	StatusBusyBit       = 0x1
	StatusBootModeBit   = 0x2
	StatusBootStatusBit = 0x4
	StatusErrorMask     = 0xFF00
)

type SPI struct {
	closed int32
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

func (s *SPI) GetFlag(op uint, arg *uint64) error {
	if atomic.LoadInt32(&s.closed) != 0 {
		return io.ErrClosedPipe
	}
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.ioctl(op|0x80000000, unsafe.Pointer(arg))
}

func (s *SPI) SetFlag(op uint, arg uint64) error {
	if atomic.LoadInt32(&s.closed) != 0 {
		return io.ErrClosedPipe
	}
	s.lock.Lock()
	defer s.lock.Unlock()
	if err := s.ioctl(op|0x40000000, unsafe.Pointer(&arg)); err != nil {
		return err
	}
	actual := uint64(0)
	// GetFlag() without lock.
	if err := s.ioctl(op|0x80000000, unsafe.Pointer(&actual)); err != nil {
		return err
	}
	if actual != arg {
		return fmt.Errorf("spi op 0x%x: set 0x%x, read 0x%x", op, arg, actual)
	}
	return nil
}

func (s *SPI) Read(b []byte) (int, error) {
	if atomic.LoadInt32(&s.closed) != 0 {
		return 0, io.ErrClosedPipe
	}
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.f.Read(b)
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
// It's big endian.
type I2C struct {
	closed int32
	lock   sync.Mutex
	f      *os.File
}

func MakeI2C() (*I2C, error) {
	// TODO(maruel): Use device tree instead of old style i2c-dev fake device.
	//
	// See "Method 4" of
	// https://www.kernel.org/doc/Documentation/i2c/instantiating-devices
	//
	// Running:
	//     echo "lepton 0x2A" | sudo tee /sys/bus/i2c/devices/i2c-1/new_device
	//
	// Creates /sys/class/i2c-adapter/i2c-1/1-002a/ which could be used to
	// communicate with the device. The goal is to remove the need for
	// /dev/i2c-1 created by driver i2c-dev.
	/*
		root := "/sys/class/i2c-adapter"
		files, err := ioutil.ReadDir(root)
		if len(files) == 0 {
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("failed to find an i2c adapter in %s", root)
		}
		path := filepath.Join(root, files[0].Name(), "device")
	*/
	path := fmt.Sprintf("/dev/i2c-%v", 1)
	f, err := os.OpenFile(path, os.O_RDWR, os.ModeExclusive)
	if err != nil {
		return nil, err
	}
	i := &I2C{f: f}
	if err := i.setAddress(i2cAddress); err != nil {
		f.Close()
		return nil, err
	}

	// Wait for the device to be booted.
	for {
		status, err := i.waitIdle()
		if err != nil {
			f.Close()
			return nil, err
		}
		if status == StatusBootStatusBit|StatusBootModeBit {
			break
		}
		log.Printf("i2c: lepton not yet booted: 0x%02x", status)
	}
	return i, nil
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

func (i *I2C) GetAttribute(command Command, result []uint16) error {
	if len(result) > 1024 {
		return errors.New("buffer too large")
	}
	if atomic.LoadInt32(&i.closed) != 0 {
		return io.ErrClosedPipe
	}
	i.lock.Lock()
	defer i.lock.Unlock()
	if _, err := i.waitIdle(); err != nil {
		return err
	}
	if err := i.writeRegister(RegDataLength, uint16(len(result))); err != nil {
		return err
	}
	if err := i.writeRegister(RegCommandID, uint16(command)); err != nil {
		return err
	}
	status, err := i.waitIdle()
	if err != nil {
		return err
	}
	if status&0xff00 != 0 {
		return fmt.Errorf("error 0x%x", status>>8)
	}
	if len(result) <= 16 {
		err = i.readData(RegData0, result)
	} else {
		err = i.readData(RegDataBuffer0, result)
	}
	if err != nil {
		return err
	}
	/* TODO(maruel): Verify CRC:
	crc, err = i.readRegister(RegDataCRC)
	if err != nil {
		return err
	}
	if expected := CalculateCRC16(result); expected != crc {
		return errors.New("invalid crc")
	}
	*/
	return nil
}

func (i *I2C) SetAttribute(command Command, value []uint16) error {
	if len(value) > 1024 {
		return errors.New("buffer too large")
	}
	if atomic.LoadInt32(&i.closed) != 0 {
		return io.ErrClosedPipe
	}
	i.lock.Lock()
	defer i.lock.Unlock()
	if _, err := i.waitIdle(); err != nil {
		return err
	}
	var err error
	if len(value) <= 16 {
		err = i.writeData(RegData0, value)
	} else {
		err = i.writeData(RegDataBuffer0, value)
	}
	if err != nil {
		return err
	}
	if err := i.writeRegister(RegDataLength, uint16(len(value))); err != nil {
		return err
	}
	if err := i.writeRegister(RegCommandID, uint16(command)|1); err != nil {
		return err
	}
	status, err := i.waitIdle()
	if err != nil {
		return err
	}
	if status&0xff00 != 0 {
		return fmt.Errorf("error 0x%x", status>>8)
	}
	return nil
}

func (i *I2C) RunCommand(command Command) error {
	if atomic.LoadInt32(&i.closed) != 0 {
		return io.ErrClosedPipe
	}
	i.lock.Lock()
	defer i.lock.Unlock()
	if _, err := i.waitIdle(); err != nil {
		return err
	}
	if err := i.writeRegister(RegDataLength, 0); err != nil {
		return err
	}
	if err := i.writeRegister(RegCommandID, uint16(command)|2); err != nil {
		return err
	}
	status, err := i.waitIdle()
	if err != nil {
		return err
	}
	if status&0xff00 != 0 {
		return fmt.Errorf("error 0x%x", status>>8)
	}
	return nil
}

// Private details.

// Drivers IOCTL control codes.
const (
	spiIOCMode        = 0x16B01
	spiIOCBitsPerWord = 0x16B03
	spiIOCMaxSpeedHz  = 0x46B04

	i2cAddress       = 0x2A
	i2cIOCSetAddress = 0x0703 // I2C_SLAVE
)

func (i *I2C) setAddress(address byte) error {
	return i.ioctl(i2cIOCSetAddress, uintptr(address))
}

func (i *I2C) read(b []byte) (int, error) {
	if len(b)&1 != 0 {
		panic("lepton CCI requires 16 bits aligned read")
	}
	n, err := i.f.Read(b)
	if err == nil && n != len(b) {
		err = io.ErrShortBuffer
	}
	return n, err
}

func (i *I2C) write(b []byte) (int, error) {
	if len(b)&1 != 0 {
		panic("lepton CCI requires 16 bits aligned write")
	}
	n, err := i.f.Write(b)
	return n, err
}

func (i *I2C) ioctl(op uint, arg uintptr) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, i.f.Fd(), uintptr(op), arg); errno != 0 {
		return fmt.Errorf("i2c ioctl: %s", syscall.Errno(errno))
	}
	return nil
}

// waitIdle waits for camera to be ready.
func (i *I2C) waitIdle() (uint16, error) {
	for {
		value, err := i.readRegister(RegStatus)
		if err != nil || value&StatusBusyBit == 0 {
			return value, err
		}
		log.Printf("i2c.waitIdle(): device busy %x", value)
		time.Sleep(5 * time.Millisecond)
	}
}

func (i *I2C) readRegister(addr RegisterAddress) (uint16, error) {
	data := []uint16{0}
	err := i.readData(addr, data)
	return data[0], err
}

func (i *I2C) readData(addr RegisterAddress, data []uint16) error {
	if atomic.LoadInt32(&i.closed) != 0 {
		return io.ErrClosedPipe
	}
	if _, err := i.write([]byte{byte(addr >> 8), byte(addr & 0xff)}); err != nil {
		return err
	}
	tmp := make([]byte, len(data)*2)
	if _, err := i.read(tmp); err != nil {
		return err
	}
	for i := range data {
		data[i] = uint16(tmp[2*i])<<8 | uint16(tmp[2*i+1])
	}
	return nil
}

func (i *I2C) writeRegister(addr RegisterAddress, data uint16) error {
	return i.writeData(addr, []uint16{data})
}

func (i *I2C) writeData(addr RegisterAddress, data []uint16) error {
	if atomic.LoadInt32(&i.closed) != 0 {
		return io.ErrClosedPipe
	}
	tmp := make([]byte, len(data)*2+2)
	tmp[0] = byte(addr >> 8)
	tmp[1] = byte(addr & 0xff)
	for i, d := range data {
		tmp[2*i+2] = byte(d >> 8)
		tmp[2*i+3] = byte(d & 0xff)
	}
	_, err := i.write(tmp)
	return err
}
