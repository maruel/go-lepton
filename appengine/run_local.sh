#!/usr/bin/env bash
# Copyright 2015 Marc-Antoine Ruel. All rights reserved.
# Use of this source code is governed under the Apache License, Version 2.0
# that can be found in the LICENSE file.

BASEDIR="$(dirname $0)"

~/src/go_appengine/goapp serve -host 0.0.0.0 $BASEDIR/seeall
