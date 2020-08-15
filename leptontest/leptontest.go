// Copyright 2015 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

// Package leptontest implements a fake Lepton implementation.
package leptontest

import (
	"image"
	"math/rand"
	"time"

	"periph.io/x/periph/conn/physic"
	"periph.io/x/periph/devices/lepton"
	"periph.io/x/periph/devices/lepton/cci"
	"periph.io/x/periph/devices/lepton/image14bit"
)

// Lepton reads and controls a FLIR Lepton. This interface can be mocked.
type Lepton interface {
	GetFFCModeControl() (*cci.FFCMode, error)
	GetSerial() (uint64, error)
	GetShutterPos() (cci.ShutterPos, error)
	GetStatus() (*cci.Status, error)
	GetTemp() (physic.Temperature, error)
	GetTempHousing() (physic.Temperature, error)
	GetUptime() (time.Duration, error)
	NextFrame(img *lepton.Frame) error
	Bounds() image.Rectangle
	RunFFC() error
}

// LeptonFake is a fake for lepton.Lepton.
type LeptonFake struct {
	noise *noise
	last  *lepton.Frame
	start time.Time
}

// New returns a mock for lepton.Lepton.
func New() (*LeptonFake, error) {
	last := &lepton.Frame{Gray14: image14bit.NewGray14(image.Rect(0, 0, 80, 60))}
	return &LeptonFake{noise: makeNoise(), last: last, start: time.Now().UTC()}, nil
}

func (l *LeptonFake) NextFrame(img *lepton.Frame) error {
	// ~9hz
	time.Sleep(111 * time.Millisecond)
	img.Metadata.FrameCount = l.last.Metadata.FrameCount + 1
	img.Metadata.Temp = physic.ZeroCelsius
	l.noise.update()
	l.noise.render(img)
	l.last = img
	return nil
}

func (l *LeptonFake) Bounds() image.Rectangle {
	return image.Rect(0, 0, 80, 60)
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

func (l *LeptonFake) GetTemp() (physic.Temperature, error) {
	return physic.Celsius + physic.ZeroCelsius, nil
}

func (l *LeptonFake) GetTempHousing() (physic.Temperature, error) {
	return physic.ZeroCelsius, nil
}

func (l *LeptonFake) GetShutterPos() (cci.ShutterPos, error) {
	return cci.ShutterPosIdle, nil
}

func (l *LeptonFake) GetFFCModeControl() (*cci.FFCMode, error) {
	return &cci.FFCMode{}, nil
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
			f.SetIntensity14(x, y, image14bit.Intensity14(value))
			avg += int32(value)
		}
	}
	f.Metadata.AvgValue = uint16(avg / (80 * 60))
}
