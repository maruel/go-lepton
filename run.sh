#!/bin/sh
# Copyright 2015 Marc-Antoine Ruel. All rights reserved.
# Use of this source code is governed under the Apache License, Version 2.0
# that can be found in the LICENSE file.

# Syncs and runs itself.

BASEDIR=$(dirname $0)
go get -u github.com/maruel/go-lepton/lepton
lepton

# Start again in case of crash.
./run.sh
