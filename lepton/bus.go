// Copyright 2015 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

// Do not use embd because its SPI and i²c implementations are too slow.

package lepton

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"sync/atomic"
	"time"
)

// Command to be sent over i²c.
type Command uint16

// All the available commands.
const (
	AgcEnable                 Command = 0x0100 // 2   GET/SET
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
	SysFFCMode                Command = 0x023C // 17  GET/SET Manual control; doc says 20 words but it's 17 in practice.
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

///

func MakeI2CLepton() (*I2C, error) {
	i, err := MakeI2C()
	if err != nil {
		return nil, err
	}
	if err := i.ioctl(i2cIOCSetAddress, uintptr(i2cLeptonAddress)); err != nil {
		i.f.Close()
		return nil, err
	}

	// Wait for the device to be booted.
	for {
		status, err := i.waitIdle()
		if err != nil {
			i.f.Close()
			return nil, err
		}
		if status == StatusBootStatusBit|StatusBootModeBit {
			break
		}
		log.Printf("i2c: lepton not yet booted: 0x%02x", status)
		time.Sleep(5 * time.Millisecond)
	}
	return i, nil
}

func (i *I2C) GetAttribute(command Command, data interface{}) error {
	//log.Printf("GetAttribute(%s, %s)", command, reflect.TypeOf(data).String())
	nbWords := binary.Size(data) / 2
	if nbWords > 1024 {
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
	if err := i.writeRegister(RegDataLength, uint16(nbWords)); err != nil {
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
	b := make([]byte, nbWords*2)
	if nbWords <= 16 {
		err = i.readData(RegData0, b)
	} else {
		err = i.readData(RegDataBuffer0, b)
	}
	if err != nil {
		return err
	}
	if err := binary.Read(bytes.NewBuffer(b), binary.LittleEndian, data); err != nil {
		return err
	}
	//log.Printf("GetAttribute(%s, %s) = %#v", command, reflect.TypeOf(data).String(), reflect.ValueOf(data).Elem().Interface())
	/*
		// TODO(maruel): Verify CRC:
		crc, err := i.readRegister(RegDataCRC)
		if err != nil {
			return err
		}
		if expected := crc16.ChecksumCCITT(b); expected != crc {
			return fmt.Errorf("invalid crc; expected 0x%04X; got 0x%04X", expected, crc)
		}
	*/
	return nil
}

func (i *I2C) SetAttribute(command Command, data interface{}) error {
	//log.Printf("SetAttribute(%s, %#v)", command, data)
	buf := &bytes.Buffer{}
	if err := binary.Write(buf, binary.LittleEndian, data); err != nil {
		return err
	}
	b := buf.Bytes()
	nbWords := len(b) / 2
	if nbWords > 1024 {
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
	if nbWords <= 16 {
		err = i.writeData(RegData0, b)
	} else {
		err = i.writeData(RegDataBuffer0, b)
	}
	if err != nil {
		return err
	}
	if err := i.writeRegister(RegDataLength, uint16(nbWords)); err != nil {
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

const (
	i2cLeptonAddress = 0x2A // Hardcoded value for the Lepton.
)

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
