// Copyright 2015 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"image"
	"image/png"
	"log"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"runtime/pprof"
	"sync"
	"time"

	"github.com/maruel/go-lepton/appengine/seeall/api"
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
	lock     sync.Mutex
	Img      *lepton.LeptonBuffer
	Stats    lepton.Stats
	ImgsSent int
	HTTPReqs int
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
	if err := rootTmpl.Execute(w, currentState); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	currentState.lock.Unlock()
}

func still(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	img := image.NewGray(image.Rect(0, 0, 80, 60))
	currentState.lock.Lock()
	currentState.Img.Scale(img)
	currentState.lock.Unlock()
	if err := png.Encode(w, img); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func still16(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	currentState.lock.Lock()
	defer currentState.lock.Unlock()
	if err := png.Encode(w, currentState.Img); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func sendImages(c <-chan *lepton.LeptonBuffer, ring *imageRing) {
	/*
		// Disable compression because the bulk of data is PNGs and the CPU is slow.
		var t http.Transport = http.DefaultTransport
		t.DisableCompression = true
		client := &http.Client{Transport: t}
	*/
	client := &http.Client{}

	imgs := make([]*lepton.LeptonBuffer, 0, 9*5)
	for {
		// Reap as much as possible.
		imgs = imgs[:0]
		loop := true
		for loop {
			select {
			case i := <-c:
				imgs = append(imgs, i)
			case <-interrupt.Channel:
				return
			default:
				loop = false
			}
		}
		if len(imgs) == 0 {
			continue
		}
		currentState.lock.Lock()
		ring.done(currentState.Img)
		currentState.Img = imgs[len(imgs)-1]
		currentState.lock.Unlock()

		sendImgs(client, imgs)
		currentState.lock.Lock()
		currentState.ImgsSent += len(imgs)
		currentState.HTTPReqs++
		currentState.lock.Unlock()

		for i := 0; i < len(imgs)-1; i++ {
			ring.done(imgs[i])
		}
	}
}

func sendImgs(client *http.Client, imgs []*lepton.LeptonBuffer) {
	req := &api.PushRequest{
		ID:     Config.ID,
		Secret: Config.Secret,
		Items:  make([]api.PushRequestItem, len(imgs)),
	}
	now := time.Now().UTC()
	var w bytes.Buffer
	for i, img := range imgs {
		if err := png.Encode(&w, img); err != nil {
			panic(err)
		}
		req.Items[i].Timestamp = now
		req.Items[i].PNG = w.Bytes()
		w.Reset()
	}
	if err := json.NewEncoder(&w).Encode(req); err != nil {
		panic(err)
	}
	url := "https://" + Config.Server + "/api/seeall/v1/push"
	resp, err := http.Post(url, "application/json", &w)
	if err != nil {
		log.Printf("Failed to post image: %s", err)
	} else {
		// TODO(maruel): Read response.
		resp.Body.Close()
	}
}

var Config = struct {
	ID     int64
	Secret string
	Server string
}{}

func mainImpl() error {
	cpuprofile := flag.String("cpuprofile", "", "dump CPU profile in file")
	port := flag.Int("port", 8010, "http port to listen on")
	writeConfig := flag.Bool("writeConfig", false, "write an empty config file and exit")
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

	usr, _ := user.Current()
	configDir := filepath.Join(usr.HomeDir, ".config", "lepton")
	configPath := filepath.Join(configDir, "lepton.json")
	if f, err := os.Open(configPath); err == nil {
		if err := json.NewDecoder(f).Decode(&Config); err != nil {
			f.Close()
			return err
		}
		f.Close()
	}
	if *writeConfig {
		if err := os.MkdirAll(configDir, 0700); err != nil {
			return err
		}
		f, err := os.OpenFile(configPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		defer f.Close()
		data, err := json.MarshalIndent(&Config, "", "  ")
		if err != nil {
			return err
		}
		data = append(data, '\n')
		_, err = f.Write(data)
		return err
	}

	l, err := lepton.MakeLepton()
	if l != nil {
		defer l.Close()
	}
	if err != nil {
		return err
	}

	c := make(chan *lepton.LeptonBuffer, 9*60)
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

	if Config.Server != "" {
		go sendImages(c, ring)
	} else {
		go func() {
			for {
				img := <-c
				currentState.lock.Lock()
				ring.done(currentState.Img)
				currentState.Img = img
				currentState.lock.Unlock()
			}
		}()
	}

	http.HandleFunc("/", root)
	http.HandleFunc("/favicon.ico", still)
	http.HandleFunc("/still.png", still)
	http.HandleFunc("/still16.png", still16)
	fmt.Printf("Listening on %d\n", *port)
	go http.ListenAndServe(fmt.Sprintf(":%d", *port), nil)

	for !interrupt.IsSet() {
		stats := l.Stats()
		currentState.lock.Lock()
		currentState.Stats = stats
		ImgsSent := currentState.ImgsSent
		HTTPReqs := currentState.HTTPReqs
		currentState.lock.Unlock()

		fmt.Printf("\r%d frames %d duped %d dummy %d badsync %d broken %d fail %d HTTP %d Imgs", stats.GoodFrames, stats.DuplicateFrames, stats.DummyLines, stats.SyncFailures, stats.BrokenPackets, stats.TransferFails, HTTPReqs, ImgsSent)
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
