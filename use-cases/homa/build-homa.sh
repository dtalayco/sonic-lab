#!/bin/bash
# Build Homa and its tools at a pinned commit, then load the module.
# Run as root inside the homa VM. Rerun after a VM restart to reload.
set -e
HOMA_DIR=${HOMA_DIR:-/opt/HomaModule}
HOMA_COMMIT=${HOMA_COMMIT:-4bd0275}
PATCH=$(cd "$(dirname "$0")" && pwd)/homa-arm64.patch

export DEBIAN_FRONTEND=noninteractive
apt-get install -y -qq --no-install-recommends git make gcc-13 g++ libc6-dev \
	patch python3 iputils-ping > /dev/null

[ -d $HOMA_DIR ] || git clone -q https://github.com/PlatformLab/HomaModule $HOMA_DIR
cd $HOMA_DIR
git checkout -q $HOMA_COMMIT
[ -f homa.ko ] || make -j"$(nproc)" all

# The tools assume x86: rdtsc() in test_utils.h and REG_RIP in cp_node.cc
grep -q cntvct_el0 util/test_utils.h || patch -p1 < "$PATCH"
cd util
make -j"$(nproc)" cp_node homa_prio > /dev/null
cd ..

lsmod | grep -q '^homa ' || insmod homa.ko
lsmod | grep '^homa '
echo "Homa $(git log -1 --format=%h) loaded on $(uname -r)"
