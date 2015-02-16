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
	"syscall"
	"time"
	"unsafe"
)

// Command to be sent over i²c.
type Command uint16

// All the available commands.
const (
	AGCEnable                 = Command(0x0100) // 2   GET/SET
	AgcRoiSelect              = Command(0x0108) // 4   GET/SET
	AgcHistogramStats         = Command(0x010C) // 4   GET
	AgcHeqDampFactor          = Command(0x0124) // 1   GET/SET
	AgcHeqClipLimitHigh       = Command(0x012C) // 1   GET/SET
	AgcHeqClipLimitLow        = Command(0x0130) // 1   GET/SET
	AgcHeqEmptyCounts         = Command(0x013C) // 1   GET/SET
	AgcHeqOutputScaleFactor   = Command(0x0144) // 2   GET/SET
	AgcCalculationEnable      = Command(0x0148) // 2   GET/SET
	SysPing                   = Command(0x0200) // 0   RUN
	SysStatus                 = Command(0x0204) // 4   GET
	SysSerialNumber           = Command(0x0208) // 4   GET
	SysUptime                 = Command(0x020C) // 2   GET
	SysHousingTemperature     = Command(0x0210) // 1   GET
	SysTemperature            = Command(0x0214) // 1   GET
	SysTelemetryEnable        = Command(0x0218) // 2   GET/SET
	SysTelemetryLocation      = Command(0x021C) // 2   GET/SET
	SysFlatFieldFrames        = Command(0x0224) // 2   GET/SET It's an enum
	SysCustomSerialNumber     = Command(0x0228) // 16  GET It's a string
	SysRoiSceneStats          = Command(0x022C) // 4   GET
	SysRoiSceneSelect         = Command(0x0230) // 4   GET/SET
	SysThermalShutdownCount   = Command(0x0234) // 1   GET Number of times it exceeded 80C
	SysShutterPosition        = Command(0x0238) // 2   GET/SET
	SysFFCMode                = Command(0x023C) // 20  GET/SET Manual control
	SysFCCRunNormalization    = Command(0x0240) // 0   RUN
	SysFCCStatus              = Command(0x0244) // 2   GET
	VidColorLookupSelect      = Command(0x0304) // 2   GET/SET
	VidColorLookupTransfer    = Command(0x0308) // 512 GET/SET
	VidFocusCalculationEnable = Command(0x030C) // 2   GET/SET
	VidFocusRoiSelect         = Command(0x0310) // 4   GET/SET
	VidFocusMetricThreshold   = Command(0x0314) // 2   GET/SET
	VidFocusMetricGet         = Command(0x0318) // 2   GET
	VidVideoFreezeEnable      = Command(0x0324) // 2   GET/SET
)

// RegisterAddress is a valid register that can be read or written to.
type RegisterAddress uint16

// All the available registers.
const (
	RegPower       = RegisterAddress(0)
	RegStatus      = RegisterAddress(2)
	RegCommandID   = RegisterAddress(4)
	RegDataLength  = RegisterAddress(6)
	RegData0       = RegisterAddress(8)
	RegData1       = RegisterAddress(10)
	RegData2       = RegisterAddress(12)
	RegData3       = RegisterAddress(14)
	RegData4       = RegisterAddress(16)
	RegData5       = RegisterAddress(18)
	RegData6       = RegisterAddress(20)
	RegData7       = RegisterAddress(22)
	RegData8       = RegisterAddress(24)
	RegData9       = RegisterAddress(26)
	RegData10      = RegisterAddress(28)
	RegData11      = RegisterAddress(30)
	RegData12      = RegisterAddress(32)
	RegData13      = RegisterAddress(34)
	RegData14      = RegisterAddress(36)
	RegData15      = RegisterAddress(38)
	RegDataCRC     = RegisterAddress(40)
	RegDataBuffer0 = RegisterAddress(0xF800)
	RegDataBuffer1 = RegisterAddress(0xFC00)
)

// RegStatus bitmask.
const (
	StatusBusyBit       = 0x1
	StatusBootModeBit   = 0x2
	StatusBootStatusBit = 0x4
	StatusErrorMask     = 0xFF00
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

// I2C is the Lepton specific Command and Control Interface (CCI).
//
// It's big endian.
type I2C struct {
	f *os.File
}

func MakeI2C() (*I2C, error) {
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
		status, err := i.WaitIdle()
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
	var err error
	if i.f != nil {
		err = i.f.Close()
		i.f = nil
	}
	return err
}

// Read is the low lever function. Shouldn't be used directly.
func (i *I2C) Read(b []byte) (int, error) {
	if len(b)&1 != 0 {
		panic("lepton CCI requires 16 bits aligned read")
	}
	n, err := i.f.Read(b)
	if err == nil && n != len(b) {
		err = io.ErrShortBuffer
	}
	//log.Printf("i2c.Read() = %v, %v", b, err)
	return n, err
}

// Write is the low lever function. Shouldn't be used directly.
func (i *I2C) Write(b []byte) (int, error) {
	if len(b)&1 != 0 {
		panic("lepton CCI requires 16 bits aligned write")
	}
	n, err := i.f.Write(b)
	//log.Printf("i2c.Write(%v) = %v", b, err)
	return n, err
}

// WaitIdle waits for camera to be ready.
func (i *I2C) WaitIdle() (uint16, error) {
	for {
		value, err := i.ReadRegister(RegStatus)
		if err != nil || value&StatusBusyBit == 0 {
			return value, err
		}
		log.Printf("i2c.WaitIdle(): device busy %x", value)
		time.Sleep(5 * time.Millisecond)
	}
}

func (i *I2C) GetAttribute(command Command, result []uint16) error {
	wordLength := uint16(len(result) / 2)
	if wordLength > 1024 {
		return errors.New("buffer too large")
	}
	if _, err := i.WaitIdle(); err != nil {
		return err
	}
	if err := i.WriteRegister(RegDataLength, wordLength); err != nil {
		return err
	}
	if err := i.WriteRegister(RegCommandID, uint16(command)); err != nil {
		return err
	}
	status, err := i.WaitIdle()
	if err != nil {
		return err
	}
	if status&0xff00 != 0 {
		return fmt.Errorf("error 0x%x", status>>8)
	}
	if wordLength <= 16 {
		err = i.ReadData(RegData0, result)
	} else {
		err = i.ReadData(RegDataBuffer0, result)
	}
	if err != nil {
		return err
	}
	/* TODO(maruel): Verify CRC:
	crc, err = i.ReadRegister(RegDataCRC)
	if err != nil {
		return err
	}
	if expected := CalculateCRC16(result); expected != crc {
		return errors.New("invalid crc")
	}
	*/
	return nil
}

/*
func (i *I2C) RunCommand(addr Command, in []byte, out []byte) error {
	return nil
}
*/

func (i *I2C) ReadRegister(addr RegisterAddress) (uint16, error) {
	data := []uint16{0}
	err := i.ReadData(addr, data)
	return data[0], err
}

func (i *I2C) ReadData(addr RegisterAddress, data []uint16) error {
	if _, err := i.Write([]byte{byte(addr >> 8), byte(addr & 0xff)}); err != nil {
		return err
	}
	tmp := make([]byte, len(data)*2)
	if _, err := i.Read(tmp); err != nil {
		return err
	}
	for i := range data {
		data[i] = uint16(tmp[2*i]<<8) | uint16(tmp[2*i+1])
	}
	//log.Printf("i2c.readdata(0x%02X) = %v", addr, data)
	return nil
}

func (i *I2C) WriteRegister(addr RegisterAddress, data uint16) error {
	return i.WriteData(addr, []uint16{data})
}

func (i *I2C) WriteData(addr RegisterAddress, data []uint16) error {
	tmp := make([]byte, len(data)*2+2)
	tmp[0] = byte(addr >> 8)
	tmp[1] = byte(addr & 0xff)
	for i, d := range data {
		tmp[2*i+2] = byte(d >> 8)
		tmp[2*i+3] = byte(d & 0xff)
	}
	//log.Printf("i2c.writedata(0x%02X, %v)", addr, data)
	_, err := i.Write(tmp)
	return err
}

func (i *I2C) setAddress(address byte) error {
	return i.ioctl(i2cIOCSetAddress, uintptr(address))
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

func (i *I2C) ioctl(op uint, arg uintptr) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, i.f.Fd(), uintptr(op), arg); errno != 0 {
		return fmt.Errorf("i2c ioctl: %s", syscall.Errno(errno))
	}
	return nil
}
