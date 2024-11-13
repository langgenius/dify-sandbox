This is a simple example based on electron-forge coupled with webpack, and using Koffi.

The initial structure was generated with the following command:

```sh
npm init electron-app@latest my-app -- --template=webpack
```

To run the app, execute the following:

```sh
cd examples/electron-forge
npm install
npm start
```

You can also use electron-forge to package the app directly:

```sh
cd examples/electron-forge
npm install
npm run make
```

Things should just work :)
