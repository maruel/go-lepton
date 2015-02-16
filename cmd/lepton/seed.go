// Copyright 2015 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/png"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"time"

	"github.com/maruel/go-lepton/appengine/seeall/api"
	"github.com/maruel/go-lepton/lepton"
	"github.com/maruel/interrupt"
)

type Seeder struct {
	config seederConfig
	stats  SeederStats
}

type seederConfig struct {
	ID     int64
	Secret []byte
	Server string
}

type SeederStats struct {
	ImgsSent int
	HTTPReqs int
}

func (s *seederConfig) isValid() bool {
	return s.ID != 0 && len(s.Secret) != 0 && len(s.Server) != 0
}

func (s *Seeder) Stats() SeederStats {
	// TODO(maruel): Locking.
	return s.stats
}

func (s *Seeder) sendImages(c <-chan *lepton.LeptonBuffer) {
	/*
		// Disable compression because the bulk of data is PNGs and the CPU is slow.
		var t http.Transport = http.DefaultTransport
		t.DisableCompression = true
		client := &http.Client{Transport: t}
	*/
	client := &http.Client{}

	imgs := make([]*lepton.LeptonBuffer, 0, 9*5)
	for {
		// Do not send more than 30 images at a time.
		imgs = imgs[:0]
		loop := true
		for loop && len(imgs) < 30 {
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
		s.sendImgs(client, imgs)
		s.stats.ImgsSent += len(imgs)
		s.stats.HTTPReqs++
	}
}

func (s *Seeder) sendImgs(client *http.Client, imgs []*lepton.LeptonBuffer) {
	req := &api.PushRequest{
		ID:     s.config.ID,
		Secret: s.config.Secret,
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
	url := "https://" + s.config.Server + "/api/seeall/v1/push"
	resp, err := http.Post(url, "application/json", &w)
	if err != nil {
		log.Printf("Failed to post image: %s", err)
	} else {
		// TODO(maruel): Read response.
		resp.Body.Close()
	}
}

// LoadSeeder loads ~/.config/lepton/lepton.json or create one if none exists.
func LoadSeeder() *Seeder {
	usr, _ := user.Current()
	configDir := filepath.Join(usr.HomeDir, ".config", "lepton")
	configPath := filepath.Join(configDir, "lepton.json")
	s := &Seeder{}
	var srcData []byte
	if f, err := os.Open(configPath); err == nil {
		srcData, err = ioutil.ReadAll(f)
		if err := json.Unmarshal(srcData, &s.config); err != nil {
			log.Printf("%s is invalid json: %e", configPath, err)
		}
		f.Close()
	}

	// Normalizes the config file.
	data, err := json.MarshalIndent(&s.config, "", "  ")
	if err != nil {
		panic(err)
	}
	data = append(data, '\n')
	if !bytes.Equal(srcData, data) {
		if err := os.MkdirAll(configDir, 0700); err != nil {
			log.Printf("failed to create %s: %e", configDir, err)
		}
		if f, err := os.OpenFile(configPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600); err == nil {
			if _, err = f.Write(data); err != nil {
				log.Printf("failed to write %s: %e", configPath, err)
			}
			f.Close()
		} else {
			log.Printf("failed to create %s: %e", configPath, err)
		}
	}
	if !s.config.isValid() {
		return nil
	}
	fmt.Printf("Sending to %s as ID %d\n", s.config.Server, s.config.ID)
	return s
}
