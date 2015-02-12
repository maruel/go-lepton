go-lepton
=========

Serves images taken on a FLIR Lepton connected to a Raspberry Pi SPI port to
over HTTP.

![See how glass blocks IR](https://raw.github.com/maruel/go-lepton/master/static/photo_ir.png)


Prerequisite Setup
------------------

### 1. Hardware

Connect the FLIR Lepton breakout board to the Raspberry Pi port as explained at
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


### 3. Enabling SPI and i²c

As explained at http://www.raspberrypi.org/forums/viewtopic.php?p=675658#p675658,

    sudo vi /boot/config.txt

Append this line at the end:

    dtparam=i2c_arm=on,spi=on

Then edit:

    sudo vi /etc/modprobe.d/raspi-blacklist.conf

Remove the blacklists lines:

    blacklist spi-bcm2708
    blacklist i2c-bcm2708

Then reboot:

    sudo shutdown -r now

If /dev/i2c-1 still doesn't show up, at worst run:

    sudo modprobe i2c-dev

It seems like there's a race condition between i2c-dev and i2c-bcm2708, where if
i2c-dev loads before i2c-bcm2708 is fully initialized, /dev/i2c-1 won't show up.
_To be investigated later._


### 4. Accessing SPI and i²c without root

To be able to use the SPI and i²c ports on the Raspberry Pi without root, create
a 'spi' group and add yourself to it, then add a
[udev](http://reactivated.net/writing_udev_rules.html) rule to change the ACL on
the device by running the following:

    sudo groupadd -f --system spi
    sudo adduser $USER spi
    echo 'SUBSYSTEM=="spidev", GROUP="spi"' | sudo tee --append /etc/udev/rules.d/90-spi.rules > /dev/null
    echo 'SUBSYSTEM=="i2c-dev", GROUP="spi"' | sudo tee --append /etc/udev/rules.d/90-i2c.rules > /dev/null
    sudo shutdown -r now

This removes the requirement of running random program as root just to access
the SPI port and is much saner than people who tells you to use mode="0666".
Using separate files so you can remove one or the other.


### 5. Software

It's recommended to compile directly on the device. First, you'll need git:

    sudo apt-get install git

Then visit http://dave.cheney.net/unofficial-arm-tarballs and grab the right
tarball, currently go1.4.linux-arm~multiarch-armv6-1.tar.gz. Extract it and
setup your $GOROOT and $GOCODE environment.


Installing
----------

Building go-lepton on the Raspberry Pi v1 takes ~10s which is slow but still
much faster than cross-compiling and transferring the file in.

    go get github.com/maruel/go-lepton


Performance
-----------

Reading the SPI port takes ~50% the CPU of a Raspberry Pi v1 running
Raspbian. There's a rumor about DMA based transfer but for now that's the
fastest that can be achieved.
