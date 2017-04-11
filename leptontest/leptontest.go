// Copyright 2015 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

// Package leptontest implements a fake Lepton implementation.
package leptontest

import (
	"image"
	"image/color"
	"math/rand"
	"time"

	"periph.io/x/periph/devices"

	"github.com/maruel/go-lepton/lepton"
	"github.com/maruel/go-lepton/lepton/cci"
)

// Lepton reads and controls a FLIR Lepton. This interface can be mocked.
type Lepton interface {
	GetFFCModeControl() (*cci.FFCMode, error)
	GetSerial() (uint64, error)
	GetShutterPos() (cci.ShutterPos, error)
	GetStatus() (*cci.Status, error)
	GetTemp() (devices.Celsius, error)
	GetTempHousing() (devices.Celsius, error)
	GetUptime() (time.Duration, error)
	ReadImg() (*lepton.Frame, error)
	Stats() lepton.Stats
	RunFFC() error
}

// LeptonFake is a fake for lepton.Lepton.
type LeptonFake struct {
	noise *noise
	last  *lepton.Frame
	start time.Time
	stats lepton.Stats
}

// New returns a mock for lepton.Lepton.
func New() (*LeptonFake, error) {
	last := &lepton.Frame{Gray16: image.NewGray16(image.Rect(0, 0, 80, 60))}
	return &LeptonFake{noise: makeNoise(), last: last, start: time.Now().UTC()}, nil
}

func (l *LeptonFake) ReadImg() (*lepton.Frame, error) {
	// ~9hz
	time.Sleep(111 * time.Millisecond)
	fr := &lepton.Frame{Gray16: image.NewGray16(image.Rect(0, 0, 80, 60))}
	fr.Metadata.FrameCount = l.last.Metadata.FrameCount + 1
	fr.Metadata.Temp = devices.Celsius(303000)
	l.noise.update()
	l.noise.render(fr)
	l.last = fr
	l.stats.GoodFrames++
	return fr, nil
}

func (l *LeptonFake) Close() error {
	return nil
}

func (l *LeptonFake) GetStatus() (*cci.Status, error) {
	return &cci.Status{}, nil
}

func (l *LeptonFake) GetSerial() (uint64, error) {
	return 0x1234, nil
}

func (l *LeptonFake) GetUptime() (time.Duration, error) {
	return time.Now().UTC().Sub(l.start), nil
}

func (l *LeptonFake) GetTemp() (devices.Celsius, error) {
	return devices.Celsius(303000), nil
}

func (l *LeptonFake) GetTempHousing() (devices.Celsius, error) {
	return devices.Celsius(300000), nil
}

func (l *LeptonFake) GetShutterPos() (cci.ShutterPos, error) {
	return cci.ShutterPosIdle, nil
}

func (l *LeptonFake) GetFFCModeControl() (*cci.FFCMode, error) {
	return &cci.FFCMode{}, nil
}

func (l *LeptonFake) Stats() lepton.Stats {
	return l.stats
}

func (l *LeptonFake) RunFFC() error {
	return nil
}

//

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

func (n *noise) render(f *lepton.Frame) {
	avg := int32(0)
	dynamicRange := 128
	// TODO(maruel): Stop using float64.
	for y := 0; y < 60; y++ {
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
			f.SetGray16(x, y, color.Gray16{uint16(value)})
			avg += int32(value)
		}
	}
	f.Metadata.AvgValue = uint16(avg / (80 * 60))
}
