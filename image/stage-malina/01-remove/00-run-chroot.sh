export DEBIAN_FRONTEND=noninteractive

apt-get purge -y avahi-daemon triggerhappy dphys-swapfile || true

rm -rf /etc/avahi
