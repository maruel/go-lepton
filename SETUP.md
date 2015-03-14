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

    1   2    3    4   5   6   7   8
    CS MOSI MISO CLK GND VIN SDA SCL

    1  VIN(6)   X
    3  SDA(7)   X
    5  SCL(8)   X
    7    X      X
    9    X      X
    11   X      X
    13   X      X
    15   X      X
    17   X      X
    19 MOSI(2)  X
    21 MISO(3)  X
    23 CLK(4)   CS(1)
    25 GND(5)   X


2. Updating Raspbian
--------------------

If using Raspbian, make sure to update to 3.18.x as described at
http://www.raspberrypi.org/documentation/raspbian/updating.md to use a recent
linux kernel:

    sudo apt-get update
    sudo apt-get upgrade
    sudo rpi-update
    sudo shutdown -r now


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

As explained at
http://www.raspberrypi.org/forums/viewtopic.php?p=675658#p675658, kernel 3.18.x+
disables the old drivers by default. Enable them back:

    echo '' | sudo tee --append /boot/config.txt
    echo 'dtparam=i2c_arm=on,spi=on' | sudo tee --append /boot/config.txt

And remove the kernel module blacklists:

    sudo sed -i 's/^blacklist spi-bcm2708/#blacklist spi-bcm2708/' /etc/modprobe.d/raspi-blacklist.conf
    sudo sed -i 's/^blacklist i2c-bcm2708/#blacklist i2c-bcm2708/' /etc/modprobe.d/raspi-blacklist.conf

Force the modules to be loaded in order, so /dev/i2c-1 shows up properly:

    echo '' | sudo tee --append /etc/modules
    echo 'i2c-bcm2708' | sudo tee --append /etc/modules
    echo 'i2c-dev' | sudo tee --append /etc/modules


5. Accessing SPI and i²c without root
-------------------------------------

To be able to use the SPI and i²c ports on the Raspberry Pi without root, create
a 'spi' group and add yourself to it, then add a
[udev](http://reactivated.net/writing_udev_rules.html) rule to change the ACL on
the device by running the following:

    sudo groupadd -f --system spi
    sudo adduser $USER spi
    echo 'SUBSYSTEM=="spidev", GROUP="spi"' | sudo tee /etc/udev/rules.d/90-spi.rules
    echo 'SUBSYSTEM=="i2c-dev", GROUP="spi"' | sudo tee /etc/udev/rules.d/90-i2c.rules

    # Allow all users to reboot.
    echo '%users ALL=NOPASSWD:/sbin/shutdown -r now' | sudo tee /etc/sudoers.d/reboot
    sudo chmod 0440 /etc/sudoers.d/reboot

    # Allow probing the i2c device due to the problem listed above.
    echo '%users ALL=NOPASSWD:/sbin/modprobe i2c-dev' | sudo tee /etc/sudoers.d/i2cdev
    sudo chmod 0440 /etc/sudoers.d/i2cdev

This removes the requirement of running random program as root just to access
the SPI port and is much saner than people who tells you to use mode="0666" (!).
You're done! You can reboot now:

    sudo shutdown -r now


6. Software
-----------

It's recommended to compile directly on the device. First, you'll need git:

    sudo apt-get install git

Then visit http://dave.cheney.net/unofficial-arm-tarballs and grab the right
tarball, currently go1.4.linux-arm~multiarch-armv6-1.tar.gz. Extract it and
setup your $GOROOT and $GOPATH environment.
