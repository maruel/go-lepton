// Copyright 2015 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package lepton

import (
	"image"
	"time"
)

// Metadata is constructed from TelemetryRowA, which is sent at each frame.
type Metadata struct {
	SinceStartup          time.Duration //
	FrameCount            uint32        // Number of frames since the start of the camera, in 27fps (not 9fps).
	AverageValue          uint16        // Average value of the buffer.
	Temperature           CentiC        //
	TemperatureHousing    CentiC        //
	RawTemperature        uint16        //
	RawTemperatureHousing uint16        //
	FFCSince              time.Duration // Time since last FFC.
	FFCTemperature        CentiC        // Temperature at last FFC.
	FFCTemperatureHousing CentiC        //
	FFCState              FFCState      // Current FFC state, e.g. if one is happening.
	FFCDesired            bool          // Asserted at start-up, after period (default 3m) or after temperature change (default 3°K). Indicates that an FFC should be triggered as soon as possible.
	Overtemp              bool          // true 10s before self-shutdown.
}

// Frame is a Flir Lepton frame, containing 14 bits resolution intensity stored
// as image.Gray16.
//
// Values centered around 8192 accorging to camera body temperature. Effective
// range is 14 bits, so [0, 16383].
//
// Each 1 increment is approximatively 0.025°K.
type Frame struct {
	*image.Gray16
	Metadata  Metadata      // Values updated at each frame.
	Telemetry TelemetryRowA // To be removed.
}
