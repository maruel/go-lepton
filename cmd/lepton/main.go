// Copyright 2015 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"html/template"
	"image"
	"image/png"
	"net/http"
	"os"
	"runtime/pprof"
	"sync"
	"time"

	"github.com/maruel/go-lepton/lepton"
	"github.com/maruel/interrupt"
)

type imageRing struct {
	c chan *lepton.LeptonBuffer
}

func makeImageRing() *imageRing {
	return &imageRing{c: make(chan *lepton.LeptonBuffer, 16)}
}

func (i *imageRing) get() *lepton.LeptonBuffer {
	select {
	case b := <-i.c:
		return b
	default:
		return &lepton.LeptonBuffer{}
	}
}

func (i *imageRing) done(b *lepton.LeptonBuffer) {
	if len(i.c) < 8 {
		i.c <- b
	}
}

type doubleBuffer struct {
	lock        sync.Mutex
	frontBuffer *image.Gray
	backBuffer  *image.Gray
	Stats       lepton.Stats
	Min         uint16
	Max         uint16
}

var currentImage doubleBuffer

var rootTmpl = template.Must(template.New("name").Parse(`
	<html>
	<head>
	<title>go-lepton</title>
	<script>
	function reload() {
		var still = document.getElementById("still");
		still.src = "/still.png#" + new Date().getTime();
	}
	</script>
	</head>
	<body>
	Still:<br>
	<a href="/still.png"><img id="still" src="/still.png" onload="reload()"></img></a>
	<br>
	{{.Stats}}
	<br>
	{{.Min}} - {{.Max}}
	</body>
	</html>`))

func root(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	currentImage.lock.Lock()
	rootTmpl.Execute(w, currentImage)
	currentImage.lock.Unlock()
}

func still(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	currentImage.lock.Lock()
	defer currentImage.lock.Unlock()
	png.Encode(w, currentImage.frontBuffer)
}

func mainImpl() error {
	cpuprofile := flag.String("cpuprofile", "", "dump CPU profile in file")
	port := flag.Int("port", 8010, "http port to listen on")
	flag.Parse()

	if len(flag.Args()) != 0 {
		return fmt.Errorf("unexpected argument: %s", flag.Args())
	}

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			return err
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	interrupt.HandleCtrlC()

	l, err := lepton.MakeLepton()
	if l != nil {
		defer l.Close()
	}
	if err != nil {
		return err
	}

	c := make(chan *lepton.LeptonBuffer, 16)

	currentImage.frontBuffer = image.NewGray(image.Rect(0, 0, 80, 60))
	currentImage.backBuffer = image.NewGray(image.Rect(0, 0, 80, 60))
	ring := makeImageRing()

	go func() {
		for {
			// Keep this loop busy to not lose sync on SPI.
			b := ring.get()
			l.ReadImg(b)
			c <- b
		}
	}()

	go func() {
		for {
			// Processing is done in a separate loop to not miss a frame.
			img := <-c
			lepton.Scale(currentImage.backBuffer, img)
			ring.done(img)
			currentImage.lock.Lock()
			currentImage.backBuffer, currentImage.frontBuffer = currentImage.frontBuffer, currentImage.backBuffer
			currentImage.Min = img.Min
			currentImage.Max = img.Max
			currentImage.lock.Unlock()
		}
	}()

	http.HandleFunc("/", root)
	http.HandleFunc("/favicon.ico", still)
	http.HandleFunc("/still.png", still)
	fmt.Printf("Listening on %d\n", *port)
	go http.ListenAndServe(fmt.Sprintf(":%d", *port), nil)

	for !interrupt.IsSet() {
		stats := l.Stats()
		currentImage.lock.Lock()
		currentImage.Stats = stats
		currentImage.lock.Unlock()
		fmt.Printf("\r%d frames %d duped %d dummy %d badsync %d broken %d fail", stats.GoodFrames, stats.DuplicateFrames, stats.DummyLines, stats.SyncFailures, stats.BrokenPackets, stats.TransferFails)
		time.Sleep(time.Second)
	}
	fmt.Print("\n")
	return nil
}

func main() {
	if err := mainImpl(); err != nil {
		fmt.Fprintf(os.Stderr, "\ngo-lepton: %s.\n", err)
		os.Exit(1)
	}
}
