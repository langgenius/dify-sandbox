#!/bin/bash
# Setup sandbox users - Single or Batch mode
set -eu

if [ "$(id -u)" -ne 0 ]; then
    echo "Please run as root"
    exit 1
fi

# Single user setup
setup_single() {
    local user="sandbox" uid=65537
    if ! id "$user" &>/dev/null; then
        useradd -u "$uid" -d /nonexistent -s /usr/sbin/nologin "$user"
        echo "Created $user (UID: $uid, GID: $(id -g "$user"))"
    else
        echo "User $user already exists"
    fi
}

# Batch users setup
setup_batch() {
    local min=${1:-10000} max=${2:-11000}
    local backup="/etc/passwd.backup.$(date +%Y%m%d_%H%M%S)"
    
    cp /etc/passwd "$backup"
    
    # Remove existing sandbox entries
    sed -i '/^sandbox[0-9]\+:/d' /etc/passwd
    
    for i in $(seq $min $((max-1))); do
        echo "sandbox${i}:x:${i}:0::/nonexistent:/usr/sbin/nologin" >> /etc/passwd
    done
    
    echo "Created $((max-min)) entries ($min-$((max-1)))"
}


echo "=== Setting up single sandbox user ==="
setup_single

echo -e "\n=== Setting up batch sandbox users ==="
setup_batch

echo -e "\n=== Setup complete ==="