// Copyright 2017 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package lepton

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/maruel/go-lepton/lepton/internal"

	"periph.io/x/periph/conn/gpio/gpiotest"
	"periph.io/x/periph/conn/i2c/i2ctest"
	"periph.io/x/periph/conn/spi/spitest"
)

func TestNew_cs(t *testing.T) {
	i := i2ctest.Playback{
		Ops: append(initSequence(),
			[]i2ctest.IO{
				{Addr: 42, W: []byte{0x0, 0x2}, R: []byte{0x0, 0x6}}, // waitIdle
				{Addr: 42, W: []byte{0x0, 0x6, 0x0, 0x0}},
				{Addr: 42, W: []byte{0x0, 0x4, 0x48, 0x2}},
				{Addr: 42, W: []byte{0x0, 0x2}, R: []byte{0x0, 0x6}}, // waitIdle
			}...),
	}
	s := spitest.Playback{}
	d, err := New(&s, &i, &gpiotest.Pin{N: "CS"})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Halt(); err != nil {
		t.Fatal(err)
	}
	if err := i.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestNew(t *testing.T) {
	i := i2ctest.Playback{Ops: initSequence()}
	s := spitest.Playback{CSPin: &gpiotest.Pin{N: "CS"}}
	_, err := New(&s, &i, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := i.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestNew_fail(t *testing.T) {
	i := i2ctest.Record{}
	s := spitest.Record{}
	if _, err := New(&s, &i, nil); err == nil {
		t.Fatal("no CS")
	}
}

/*
func TestReadImg(t *testing.T) {
	i := i2ctest.Playback{
		Ops: append(initSequence(),
			[]i2ctest.IO{
				{Addr: 42, W: []byte{0x0, 0x2}, R: []byte{0x0, 0x6}}, // waitIdle
				{Addr: 42, W: []byte{0x0, 0x6, 0x0, 0x0}},
				{Addr: 42, W: []byte{0x0, 0x4, 0x48, 0x2}},
				{Addr: 42, W: []byte{0x0, 0x2}, R: []byte{0x0, 0x6}}, // waitIdle
			}...),
	}
	s := spitest.Playback{
		Playback: conntest.Playback{
			Ops: []conntest.IO{
				{R: make([]byte, 164*8)},
				{R: make([]byte, 164*8)},
			},
			DontPanic: true,
		},
	}
	d, err := New(&s, &i, &gpiotest.Pin{N: "CS"})
	if err != nil {
		t.Fatal(err)
	}
	d.ReadImg()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}
*/

func TestParseTelemetry_fail(t *testing.T) {
	l := telemetryLine(t)
	m := Metadata{}
	if m.parseTelemetry(l[:len(l)-1]) == nil {
		t.Fatal("buffer too short")
	}
	buf := bytes.Buffer{}
	rowA := internal.TelemetryRowA{StatusBits: statusMaskNil}
	if err := binary.Write(&buf, internal.Big16, &rowA); err != nil {
		t.Fatal(err)
	}
	if m.parseTelemetry(buf.Bytes()) == nil {
		t.Fatal("bad status")
	}
}

func TestParseTelemetry(t *testing.T) {
	m := Metadata{}
	if err := m.parseTelemetry(telemetryLine(t)); err != nil {
		t.Fatal(err)
	}

	data := []struct {
		rowA    internal.TelemetryRowA
		success bool
	}{
		{internal.TelemetryRowA{TelemetryRevision: 8, StatusBits: 0 << statusFFCStateShift}, true},
		{internal.TelemetryRowA{TelemetryRevision: 8, StatusBits: 1 << statusFFCStateShift}, true},
		{internal.TelemetryRowA{TelemetryRevision: 8, StatusBits: 2 << statusFFCStateShift}, true},
		{internal.TelemetryRowA{TelemetryRevision: 8, StatusBits: 3 << statusFFCStateShift}, false},
		{internal.TelemetryRowA{StatusBits: 0 << statusFFCStateShift}, true},
		{internal.TelemetryRowA{StatusBits: 1 << statusFFCStateShift}, false},
		{internal.TelemetryRowA{StatusBits: 2 << statusFFCStateShift}, true},
		{internal.TelemetryRowA{StatusBits: 3 << statusFFCStateShift}, true},
	}
	for _, line := range data {
		buf := bytes.Buffer{}
		if err := binary.Write(&buf, internal.Big16, &line.rowA); err != nil {
			t.Fatal(err)
		}
		err := m.parseTelemetry(buf.Bytes())
		if line.success {
			if err != nil {
				t.Fatal(err)
			}
		} else {
			if err == nil {
				t.Fatal("expected failure")
			}
		}
	}
}

//

func initSequence() []i2ctest.IO {
	return []i2ctest.IO{
		{Addr: 42, W: []byte{0, 2}, R: []byte{0, 6}},                   // waitIdle
		{Addr: 42, W: []byte{0, 2}, R: []byte{0, 6}},                   // waitIdle
		{Addr: 42, W: []byte{0, 6, 0, 4}},                              // GetStatus()
		{Addr: 42, W: []byte{0, 4, 2, 4}},                              //
		{Addr: 42, W: []byte{0, 2}, R: []byte{0, 6}},                   // waitIdle
		{Addr: 42, W: []byte{0, 8}, R: []byte{0, 0, 0, 0, 0, 0, 0, 0}}, // GetStatus() result
		{Addr: 42, W: []byte{0, 2}, R: []byte{0, 6}},                   // waitIdle
		{Addr: 42, W: []byte{0, 8, 0, 0, 0, 0}},                        // Init()
		{Addr: 42, W: []byte{0, 6, 0, 0x2}},                            //
		{Addr: 42, W: []byte{0, 4, 1, 0x1}},                            //
		{Addr: 42, W: []byte{0, 2}, R: []byte{0, 6}},                   // waitIdle
		{Addr: 42, W: []byte{0, 2}, R: []byte{0, 6}},                   // waitIdle
		{Addr: 42, W: []byte{0, 8, 0, 1, 0, 0}},                        //
		{Addr: 42, W: []byte{0, 6, 0, 2}},                              //
		{Addr: 42, W: []byte{0, 4, 2, 0x19}},                           //
		{Addr: 42, W: []byte{0, 2}, R: []byte{0, 6}},                   // waitIdle
		{Addr: 42, W: []byte{0, 2}, R: []byte{0, 6}},                   // waitIdle
		{Addr: 42, W: []byte{0, 8, 0, 0, 0, 0}},                        //
		{Addr: 42, W: []byte{0, 6, 0, 2}},                              //
		{Addr: 42, W: []byte{0, 4, 2, 0x1d}},                           // Init() end
		{Addr: 42, W: []byte{0, 2}, R: []byte{0, 6}},                   // waitIdle
	}
}

func telemetryLine(t *testing.T) []byte {
	b := bytes.Buffer{}
	rowA := internal.TelemetryRowA{TelemetryRevision: 8}
	if err := binary.Write(&b, internal.Big16, &rowA); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
