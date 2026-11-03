#!/bin/bash
# Create the homa Colima profile and switch its kernel to one Homa builds
# against. Run on the Mac. Leaves the docker CLI pointing at the homa VM;
# `docker context use colima` switches back to the default profile.
set -e
PROFILE=${PROFILE:-homa}
KERNEL=${KERNEL:-6.17.0-42-generic}

colima start $PROFILE --vm-type vz --cpu 6 --memory 10 --disk 60 --mount-type virtiofs
colima ssh -p $PROFILE -- sudo env DEBIAN_FRONTEND=noninteractive \
	apt-get install -y -qq linux-image-$KERNEL linux-modules-extra-$KERNEL linux-headers-$KERNEL
colima stop $PROFILE
colima start $PROFILE
colima ssh -p $PROFILE -- uname -r
