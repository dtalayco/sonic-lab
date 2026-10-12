# sonic-vs for arm64

This directory contains a patch you need to build sonic-buildimage for arm64.
Here is how you do that. This is assumed to be done on a Mac with an M-series
chip.

## Create a multipass VM

```bash
brew install multipass
multipass launch --name sonic-builder --cpus 4 --memory 16G --disk 100G 24.04
```

## Instructions to build sonic-buildiamge for arm64

Copy the patch file into the VM:

```bash
multipass transfer sonic-patches/sonic-arm64.diff sonic-builder:
```

Inside your multipass VM (`multipass shell sonic-builder`) run the below:

```bash
sudo apt update && sudo apt install -y docker.io python3-pip
git clone https://github.com/sonic-net/sonic-buildimage.git
cd sonic-buildimage
git apply ~/sonic-arm64.diff
pip3 install jinjanator --break-system-packages
sudo file=/usr/bin/j2 bash -c 'cat << "EOF" > "$file"
#!/usr/bin/env python3
import sys
from jinjanator.cli import main
if __name__ == "__main__":
    sys.exit(main())
EOF'
sudo chmod +x /usr/bin/j2
hash -r
make init
make configure PLATFORM=vs PLATFORM_ARCH=arm64
make SONIC_BUILD_JOBS=4 target/docker-sonic-vs.gz
ls -lrt target/docker-sonic-vz.gz
```

## Copy docker-sonic-vs out and load into colima

Now you need to get the built docker-sonic-vs image for arm64 out of the
multipass VM and load it into colima:

```bash
multipass transfer sonic-builder:/home/ubuntu/sonic-buildimage/target/docker-sonic-vs.gz ~/
docker load < ~/docker-sonic-vs.gz
```
