// Copyright 2015 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

// "stringer" can be installed with "go get golang.org/x/tools/cmd/stringer"
//go:generate stringer -output=strings_gen.go -type=CameraStatus,Command,FFCShutterMode,FFCState,Flag,RegisterAddress,ShutterPosition,ShutterTempLockoutState,TelemetryLocation

package lepton

import (
	"fmt"
	"io"
	"time"
)

// Lepton reads and controls a FLIR Lepton. This interface can be mocked.
type Lepton interface {
	io.Closer

	GetFFCModeControl() (*FFCMode, error)             // GetFFCModeControl returns a lot of internal data.
	GetSerial() (uint64, error)                       // GetSerial returns the FLIR Lepton serial number.
	GetShutterPosition() (ShutterPosition, error)     // GetShutterPosition returns the position of the shutter if present.
	GetStatus() (*Status, error)                      // GetStatus return the status of the camera as known by the camera itself.
	GetTelemetryEnable() (Flag, error)                // GetTelemetryEnable returns if telemetry is enabled.
	GetTelemetryLocation() (TelemetryLocation, error) // GetTelemetryLocation returns if telemetry is enabled.
	GetTemperature() (CentiC, error)                  // GetTemperature returns the temperature in centi-Kelvin.
	GetTemperatureHousing() (CentiC, error)           // GetTemperatureHousing returns the temperature in centi-Kelvin.
	GetUptime() (time.Duration, error)                // GetUptime returns the uptime. Rolls over after 1193 hours.
	ReadImg() *LeptonBuffer                           // ReadImg reads an image. It is fine to call other functions concurrently to send commands to the camera.
	Stats() Stats                                     //
	TriggerFFC() error                                // TriggerFFC forces a Flat-Field Correction to be done by the camera for recalibration. It takes 23 frames and the camera runs at 27fps so it lasts less than a second.

}

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

// ShutterPosition is used with SysShutterPosition.
type ShutterPosition uint32

// Valid values for ShutterPosition.
const (
	ShutterPositionUnknown ShutterPosition = 0xFFFFFFFF // -1
	ShutterPositionIdle    ShutterPosition = 0
	ShutterPositionOpen    ShutterPosition = 1
	ShutterPositionClosed  ShutterPosition = 2
	ShutterPositionBrakeOn ShutterPosition = 3
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
	return CentiC(c)
}

// CentiC is temperature in 0.01°K but printed as °C.
type CentiC uint16

func (c CentiC) String() string {
	v := int(c) - 27315
	d := v % 100
	if d < 0 {
		d = -d
	}
	return fmt.Sprintf("%01d.%02d°C", v/100, d)
}

func (c CentiC) ToK() CentiK {
	return CentiK(c)
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
