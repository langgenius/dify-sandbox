This is a simple example shows:

- How to communicate with Koffi from a simple window (NW.js takes care of the annonying parts)
- How to use [nw-builder](https://nwutils.io/nw-builder/) to package it

To run this example, you need to install NW.js (SDK flavor) from https://nwjs.io/

```sh
cd examples/nwjs/src
npm install # Install Koffi
/path/to/nw .
```

You can also use nw-builder to package the app directly:

```sh
cd examples/nwjs
npm install
npm run pack
```
