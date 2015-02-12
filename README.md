go-lepton
=========

Serves images taken on a FLIR Lepton connected to a Raspberry Pi SPI port to
over HTTP.


Prerequisite Setup
------------------

### 1. Hardware

Connect the port as shown at
https://github.com/PureEngineering/LeptonModule/wiki


### 2. Updating Raspbian

If using Raspbian, before playing with this project, make sure to update to
3.18.x as described at
http://www.raspberrypi.org/documentation/raspbian/updating.md to use a recent
linux kernel:

    sudo apt-get update
    sudo apt-get upgrade
    sudo rpi-update
    sudo shutdown -r now


### 3. Enabling SPI

As explained at http://www.raspberrypi.org/forums/viewtopic.php?p=675658#p675658,

    sudo vi /boot/config.txt

and append these two lines at the end:

    dtparam=i2c_arm=on
    dtparam=spi=on

Then comment out the blacklist `blacklist spi-bcm2708` via

    sudo vi /etc/modprobe.d/raspi-blacklist.conf


### 4. Accessing SPI without root

To be able to use the SPI port on the Raspberry Pi without root, run the
following:

    sudo groupadd -f --system spi
    sudo adduser $USER spi
    echo 'SUBSYSTEM=="spidev", GROUP="spi"' | sudo tee --append /etc/udev/rules.d/90-spi.rules > /dev/null
    sudo shutdown -r now

This removes the requirement of running random program as root just to access
the SPI port.


### 5. Software

It's recommended to compile directly on the device. First, you'll need git:

    sudo apt-get install git

Then visit http://dave.cheney.net/unofficial-arm-tarballs and grab the right
tarball, currently go1.4.linux-arm~multiarch-armv6-1.tar.gz. Extract it and
setup your $GOROOT and $GOCODE environment.


Installing
----------

    go get github.com/maruel/go-lepton

Building go-lepton on the Raspberry Pi takes ~10s which is slow but still much
faster than cross-compiling and transferring the files in. When hacking direclty
on go-lepton, it's recommended to preinstall all libraries for maximum
compilation performance.

    cd $GOCODE/src
    go install ./...

This will make incremental build significantly faster.


Performance
-----------

Reading the SPI port takes ~50% the CPU of a Raspberry Pi v1 running
Raspbian. There's a rumor about DMA based transfer but for now that's the
fastest that can be acheived.
