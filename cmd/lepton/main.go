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

type state struct {
	lock  sync.Mutex
	Img   *lepton.LeptonBuffer
	Stats lepton.Stats
}

var currentState state

var rootTmpl = template.Must(template.New("name").Parse(`
	<html>
	<head>
		<title>go-lepton</title>
		<style>
			img.large {
				width: 480; /* Multiple of 80 */
				height: auto;
			}
		</style>
		<script>
		function reload() {
			var still = document.getElementById("still");
			still.src = "/still.png#" + new Date().getTime();
		}
		</script>
	</head>
	<body>
	Still:<br>
	<a href="/still.png"><img class="large" id="still" src="/still.png" onload="reload()"></img></a>
	<br>
	{{.Stats}}
	<br>
	{{.Img.Min}} - {{.Img.Max}}
	</body>
	</html>`))

func root(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	currentState.lock.Lock()
	rootTmpl.Execute(w, currentState)
	currentState.lock.Unlock()
}

func still(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	img := image.NewGray(image.Rect(0, 0, 80, 60))
	currentState.lock.Lock()
	currentState.Img.Scale(img)
	currentState.lock.Unlock()
	png.Encode(w, img)
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
	ring := makeImageRing()
	currentState.Img = ring.get()

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
			currentState.lock.Lock()
			ring.done(currentState.Img)
			currentState.Img = img
			currentState.lock.Unlock()
		}
	}()

	http.HandleFunc("/", root)
	http.HandleFunc("/favicon.ico", still)
	http.HandleFunc("/still.png", still)
	fmt.Printf("Listening on %d\n", *port)
	go http.ListenAndServe(fmt.Sprintf(":%d", *port), nil)

	for !interrupt.IsSet() {
		stats := l.Stats()
		currentState.lock.Lock()
		currentState.Stats = stats
		currentState.lock.Unlock()
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
