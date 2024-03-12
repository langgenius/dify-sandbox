This is a simple example to bundle a CLI node.js app that uses Koffi, using esbuild.

To run the app, execute the following:

```sh
cd examples/node-esbuild
npm install
npm start
```

You can bundle the script and the native modules with the following command

```sh
cd examples/node-esbuild
npm install
npm run bundle
```

Internally, this uses the esbuild copy loader to handle the native `.node` modules.
