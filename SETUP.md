go-lepton - Prerequisite Setup
==============================

This page contains all the prerequisites to make the project work without
running as root.

1. Hardware
-----------

Connect the FLIR Lepton breakout board to the Raspberry Pi port as explained at
https://github.com/PureEngineering/LeptonModule/wiki

> The Lepton® module is extremely sensitive to electrostatic discharge (ESD).
> When inserting it into the breakout board be sure to use proper personal
> grounding, such as a grounding wrist strap, to prevent damaging the module.

Double-check connectors before starting up the Pi, as negative voltage would
blow up the camera. There's 2 twists, one for cables 2, 3, 4 and 5.  Another for
cables 6, 7 and 8. Here's a simpler graph of how to connect:

Breakout board:

    1   2    3    4   5   6   7   8
    CS MOSI MISO CLK GND VIN SDA SCL

Raspberry Pi with the SD Card on top and USB ports on bottom, here's where to
plug the cables on the 26 pins port on the right:

    1  VIN(6)    X
    3  SDA(7)    X
    5  SCL(8)    X
    7    X       X
    9    X       X
    11   X       X
    13   X       X
    15   X       X
    17   X       X
    19 MOSI(2)   X
    21 MISO(3)   X
    23 CLK(4)   CS(1)
    25 GND(5)    X


2. Updating Raspbian
--------------------

The following assumes [Raspbian Jessie
Lite](https://www.raspberrypi.org/downloads/raspbian/) which was released in
November 2015.

    sudo apt-get update
    sudo apt-get upgrade
    sudo shutdown -r now

You may want to take a look at https://maruel.net/post/raspberrypi-setup/ for a
quick checklist of things to do.


3. Power (optional)
-------------------

According to http://www.daveakerman.com/?page_id=1294, model A runs on 115mA
(@3.3V) and model B on 400mA (@3.3V). It's possible to save 20mA on both by
disabling HDMI output with the following command. *Do not run this if you plan
to use the HDMI port ever!*

    /opt/vc/bin/tvservice -off

Which can be run automatically via `sudo crontab -e` with prefix `@reboot`.


4. Enabling SPI and i²c
-----------------------

By default the SPI and i²c aren't loaded by default but there's a GUI to fix
that:

    sudo raspi-config

Go in `9 Advanced Options`, then `A6 SPI` enable it, then `À7 I2C`, enable it
too, then reboot.


5. Accessing SPI and i²c as an account other than 'pi'
------------------------------------------------------

To be able to use the SPI and i²c ports on the Raspberry Pi as another account
than `pi`, make sure the user is member of groups `spi` and `i2c`. The `pi` user
is member of both by default.


6. Software
-----------

It's recommended to compile directly on the device. First, you'll need git. Also
installing tmux to simplify debugging interactively the service:

    sudo apt-get install git tmux

Then visit http://dave.cheney.net/unofficial-arm-tarballs and grab the right
tarball, currently Go 1.5.3. Extract it and setup your $GOROOT and $GOPATH
environment:

    vi ~/.bash_aliases
    export GOROOT=<path to go>
    export GOPATH=$HOME
    export PATH="$PATH:$GOROOT/bin:$GOPATH/bin"


7. Start at boot
----------------

Create $HOME/start_lepton.sh with and edit as desired:

    #!/usr/bin/env bash
    source $HOME/.bash_aliases
    echo "Starting run.sh"
    mv lepton.log "lepton.log.`date --rfc-3339=seconds`"
    while true; do
      $GOPATH/src/github.com/maruel/go-lepton/run.sh &> lepton.log
    done

Then:

    chmod +x $HOME/start_lepton.sh
    sudo vi /etc/rc.local
    # Just before the 'exit 0' line, add the following line replacing pi with
    # your user account.
    su -l -c 'tmux new-session -d -s lepton -c $HOME $HOME/start_lepton.sh pi
