// Copyright 2015 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package lepton

import (
	"math/rand"
	"time"
)

type vector struct {
	intensity float64
	x         float64
	y         float64
}

// noise is cheezy but gets us going for testing without a device.
type noise struct {
	rand    *rand.Rand
	vectors []vector
}

func makeNoise() *noise {
	n := &noise{rand: rand.New(rand.NewSource(0))}
	n.vectors = make([]vector, 10)
	for i := range n.vectors {
		n.vectors[i].intensity = n.rand.NormFloat64() * 10
		n.vectors[i].x = n.rand.NormFloat64()*14 + 40
		n.vectors[i].y = n.rand.NormFloat64()*10 + 30
	}
	return n
}

func (n *noise) update() {
	for i := range n.vectors {
		n.vectors[i].intensity += n.rand.NormFloat64() * 0.1
		n.vectors[i].x += n.rand.NormFloat64() * 0.1
		n.vectors[i].y += n.rand.NormFloat64() * 0.1
	}
}

func (n *noise) render(b *LeptonBuffer) {
	avg := int32(0)
	dynamicRange := 128
	for y := 0; y < 60; y++ {
		base := y * 80
		fy := float64(y)
		for x := 0; x < 80; x++ {
			fx := float64(x)
			value := float64(8192)
			for _, vect := range n.vectors {
				distance := ((vect.x-fx)*(vect.x-fx) + (vect.y-fy)*(vect.y-fy))
				value += vect.intensity / distance
			}
			if value >= float64(8192+dynamicRange) {
				value = float64(8192 + dynamicRange)
			}
			if value < float64(8192-dynamicRange) {
				value = float64(8192 - dynamicRange)
			}
			v := uint16(value)
			b.Pix[base+x] = v
			avg += int32(v)
		}
	}
	b.Metadata.AverageValue = uint16(avg / (80 * 60))
}

type fakeLepton struct {
	noise *noise
	last  *LeptonBuffer
	start time.Time
	stats Stats
}

func MakeFakeLepton(path string, speed int) (Lepton, error) {
	last := &LeptonBuffer{}
	return &fakeLepton{noise: makeNoise(), last: last, start: time.Now().UTC()}, nil
}

func (f *fakeLepton) ReadImg() *LeptonBuffer {
	// ~9hz
	time.Sleep(111 * time.Millisecond)
	b := &LeptonBuffer{}
	b.Metadata.FrameCount = f.last.Metadata.FrameCount + 1
	f.noise.update()
	f.noise.render(b)
	f.last = b
	f.stats.GoodFrames++
	return b
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
	return time.Now().UTC().Sub(f.start), nil
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
	return f.stats
}

func (f *fakeLepton) TriggerFFC() error {
	return nil
}
