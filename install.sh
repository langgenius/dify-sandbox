# check if ubuntu/debain
if [ -f /etc/debian_version ]; then
    sudo env DEBIAN_FRONTEND=noninteractive apt-get install -y \
        -o Dpkg::Options::="--force-confdef" \
        -o Dpkg::Options::="--force-confold" \
        pkg-config gcc libseccomp-dev
# check if fedora
elif [ -f /etc/fedora-release ]; then
    sudo dnf install pkgconfig gcc libseccomp-devel
# check if arch
elif [ -f /etc/arch-release ]; then
    sudo pacman -S pkg-config gcc libseccomp
# check if alpine
elif [ -f /etc/alpine-release ]; then
    sudo apk add pkgconfig gcc libseccomp-dev
# check if centos/rhel/rocky
elif [ -f /etc/centos-release ] || [ -f /etc/redhat-release ] || [ -f /etc/rocky-release ]; then
    sudo yum install pkgconfig gcc libseccomp-devel
else
    echo "Unsupported distribution"
    exit 1
fi
