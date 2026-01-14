#! /bin/bash

# decompress nodejs
tar -xvf $NODE_TAR_XZ -C /opt
ln -s $NODE_DIR/bin/node /usr/local/bin/node
rm -f $NODE_TAR_XZ

ln -s /opt/python/bin/python3 /usr/local/bin/python3
# start main
/main
