const { contextBridge, ipcRenderer } = require("electron");

contextBridge.exposeInMainWorld("electron", {
  sendOSNotification: (title, body) => ipcRenderer.send("notify", { title, body }),
  openUrl: (url) => ipcRenderer.send("openUrl", url),
});
