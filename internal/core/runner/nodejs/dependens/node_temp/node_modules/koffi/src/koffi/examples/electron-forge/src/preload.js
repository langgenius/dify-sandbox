const { contextBridge, ipcRenderer } = require('electron');

contextBridge.exposeInMainWorld('koffi', {
    config: (...args) => ipcRenderer.invoke('koffi:config', ...args)
})
