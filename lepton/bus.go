// Copyright 2015 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

// Do not use embd because its SPI and i²c implementations are too slow.

package lepton

import (
	"fmt"
	"io"
	"log"
	"os"
	"syscall"
	"unsafe"
)

// Command to be sent over i²c.
type Command uint16

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
	if err := i.SetAddress(i2cAddress); err != nil {
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
		if status == statusBootStatusBit|statusBootModeBit {
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
		value, err := i.readRegister(i2cRegStatus)
		if err != nil || value&statusBusyBit == 0 {
			return value, err
		}
		log.Printf("i2c.WaitIdle(): device busy %x", value)
	}
}

func (i *I2C) GetAttribute(command Command, result []uint16) error {
	wordLength := uint16(len(result) / 2)
	if _, err := i.WaitIdle(); err != nil {
		return err
	}
	if err := i.writeRegister(i2cRegDataLength, wordLength); err != nil {
		return err
	}
	if err := i.writeRegister(i2cRegCommandID, uint16(command)); err != nil {
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
		err = i.readData(i2cRegData0, result)
	} else if wordLength <= 1024 {
		err = i.readData(i2cRegDataBuffer0, result)
	} else {
		panic("buffer too large")
	}
	if err != nil {
		return err
	}
	/* TODO(maruel): Verify CRC:
	crc, err = i.readRegister(i2cRegDataCRC)
	if err != nil {
		return err
	}
	if expected := CalculateCRC16(result); expected != crc {
		return errors.New("invalid crc")
	}
	*/
	return nil
}

func (i *I2C) RunCommand(addr Command, in []byte, out []byte) error {
	return nil
}

func (i *I2C) SetAddress(address byte) error {
	return i.ioctl(i2cIOCSetAddress, uintptr(address))
}

func (i *I2C) ioctl(op uint, arg uintptr) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, i.f.Fd(), uintptr(op), arg); errno != 0 {
		return fmt.Errorf("i2c ioctl: %s", syscall.Errno(errno))
	}
	return nil
}

// Private details.

type registerAddress uint16

// Drivers IOCTL control codes.
const (
	spiIOCMode        = 0x16B01
	spiIOCBitsPerWord = 0x16B03
	spiIOCMaxSpeedHz  = 0x46B04

	i2cAddress       = 0x2A
	i2cIOCSetAddress = 0x0703 // I2C_SLAVE

	i2cRegPower       = registerAddress(0)
	i2cRegStatus      = registerAddress(2)
	i2cRegCommandID   = registerAddress(4)
	i2cRegDataLength  = registerAddress(6)
	i2cRegData0       = registerAddress(8)
	i2cRegData1       = registerAddress(10)
	i2cRegData2       = registerAddress(12)
	i2cRegData3       = registerAddress(14)
	i2cRegData4       = registerAddress(16)
	i2cRegData5       = registerAddress(18)
	i2cRegData6       = registerAddress(20)
	i2cRegData7       = registerAddress(22)
	i2cRegData8       = registerAddress(24)
	i2cRegData9       = registerAddress(26)
	i2cRegData10      = registerAddress(28)
	i2cRegData11      = registerAddress(30)
	i2cRegData12      = registerAddress(32)
	i2cRegData13      = registerAddress(34)
	i2cRegData14      = registerAddress(36)
	i2cRegData15      = registerAddress(38)
	i2cRegDataCRC     = registerAddress(40)
	i2cRegDataBuffer0 = registerAddress(0xF800)
	i2cRegDataBuffer1 = registerAddress(0xFC00)

	statusBusyBit       = 0x1
	statusBootModeBit   = 0x2
	statusBootStatusBit = 0x4
)

func (i *I2C) readRegister(addr registerAddress) (uint16, error) {
	data := []uint16{0}
	err := i.readData(addr, data)
	return data[0], err
}

func (i *I2C) readData(addr registerAddress, data []uint16) error {
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

func (i *I2C) writeRegister(addr registerAddress, data uint16) error {
	return i.writeData(addr, []uint16{data})
}

func (i *I2C) writeData(addr registerAddress, data []uint16) error {
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
