go-lepton
=========

Serves images taken on a FLIR Lepton connected to a Raspberry Pi SPI port to
over HTTP.


Accessing SPI without root
--------------------------

To be able to use the SPI port on the Raspberry Pi without root, run the
following:

    sudo groupadd -f --system spi
    sudo adduser $USER spi
    echo 'SUBSYSTEM=="spidev", GROUP="spi"' | sudo tee --append /etc/udev/rules.d/90-spi.rules > /dev/null
    sudo shutdown -r now

Then connect the port as shown at
https://github.com/PureEngineering/LeptonModule/wiki

This removes the requirement of running random program as root just to access
the SPI port.


Updating Raspbian
-----------------

If using Raspbian, before playing with this project, make sure to update as
described at http://www.raspberrypi.org/documentation/raspbian/updating.md to
use a recent linux kernel:

    sudo apt-get update
    sudo apt-get upgrade
    sudo rpi-update
    sudo shutdown -r now


Performance
-----------

Reading the SPI port currently saturate the CPU of a Raspberry Pi v1 running
Raspbian.

    (pprof) top20
    21.23s of 24.38s total (87.08%)
    Dropped 71 nodes (cum <= 0.12s)
    Showing top 20 nodes out of 36 (cum >= 0.50s)
         flat  flat%   sum%        cum   cum%
       12.67s 51.97% 51.97%     14.28s 58.57%  syscall.Syscall
        1.79s  7.34% 59.31%      2.17s  8.90%  runtime.mallocgc
        0.96s  3.94% 63.25%     19.21s 78.79%  github.com/kidoman/embd/host/generic.(*spiBus).TransferAndRecieveData
        0.92s  3.77% 67.02%      0.92s  3.77%  runtime.usleep
        0.82s  3.36% 70.39%     20.03s 82.16%  main.(*Lepton).readLine
        0.46s  1.89% 72.27%      3.04s 12.47%  runtime.convT2E
        0.43s  1.76% 74.04%      0.43s  1.76%  main.(*rawBuffer).eq
        0.33s  1.35% 75.39%      0.99s  4.06%  main.(*rawBuffer).scale
        0.31s  1.27% 76.66%      0.38s  1.56%  runtime.casgstatus
        0.29s  1.19% 77.85%      0.29s  1.19%  udiv
        0.26s  1.07% 78.92%      0.77s  3.16%  runtime.reentersyscall
        0.26s  1.07% 79.98%      0.26s  1.07%  scanblock
        0.25s  1.03% 81.01%      0.25s  1.03%  ExternalCode
        0.25s  1.03% 82.03%      0.25s  1.03%  runtime.memclr
        0.23s  0.94% 82.98%      0.72s  2.95%  runtime.exitsyscall
        0.23s  0.94% 83.92%      0.23s  0.94%  runtime.memmove
        0.21s  0.86% 84.78%      0.30s  1.23%  exitsyscallfast
        0.20s  0.82% 85.60%      0.21s  0.86%  runtime.cas
        0.19s  0.78% 86.38%      0.19s  0.78%  runtime.MSpan_Sweep
        0.17s   0.7% 87.08%      0.50s  2.05%  github.com/golang/glog.V

Not sure how to improve the situation but this leaves absolutely no room for
any computation.

https://github.com/torvalds/linux/blob/master/drivers/spi/spi-bcm2835.c implies
that the FIFO buffer is 16 bytes deep but the packets sent are 164 bytes, as
required by the Lepton.
http://git.kernel.org/cgit/linux/kernel/git/torvalds/linux.git/tree/Documentation/spi/spidev
implies it's not a requirement to use ioctl() for full duplex operation and a
simple read() would be sufficent. Will be investigated later as the Lepton
doesn't read any data through the SPI port, only via i²c.
