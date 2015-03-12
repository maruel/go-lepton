// Copyright 2015 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package lepton

import (
	"time"
)

type fakeLepton struct {
}

func MakeFakeLepton(path string, speed int) (Lepton, error) {
	return &fakeLepton{}, nil
}

func (f *fakeLepton) ReadImg() *LeptonBuffer {
	// TODO(maruel): Return 2-3 stock image with some noise.
	return nil
}

// Stubs.

func (f *fakeLepton) Close() error {
	return nil
}

func (f *fakeLepton) GetStatus() (*Status, error) {
	return &Status{}, nil
}

func (f *fakeLepton) GetSerial() (uint64, error) {
	return 0x1234, nil
}

func (f *fakeLepton) GetUptime() (time.Duration, error) {
	return 3 * time.Second, nil
}

func (f *fakeLepton) GetTemperature() (CentiC, error) {
	return CentiC(30300), nil
}

func (f *fakeLepton) GetTemperatureHousing() (CentiC, error) {
	return CentiC(30000), nil
}

func (f *fakeLepton) GetTelemetryEnable() (Flag, error) {
	return Enabled, nil
}

func (f *fakeLepton) GetTelemetryLocation() (TelemetryLocation, error) {
	return Footer, nil
}

func (f *fakeLepton) GetShutterPosition() (ShutterPosition, error) {
	return ShutterPositionIdle, nil
}

func (f *fakeLepton) GetFFCModeControl() (*FFCMode, error) {
	return &FFCMode{}, nil
}

func (f *fakeLepton) Stats() Stats {
	return Stats{}
}

func (f *fakeLepton) TriggerFFC() error {
	return nil
}
