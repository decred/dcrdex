const { app, ipcMain, BrowserWindow } = require('electron')
const dexc = require('./build/Release/dexc')
const process = require('process')

Mainnet = 0
Testnet = 1
Simnet = 2

LogLevelTrace = 0
LogLevelDebug = 1
LogLevelInfo = 2
LogLevelWarn = 3
LogLevelError = 4
LogLevelCritical = 5
LogLevelOff = 6

function callDEX (func, params) {
  return dexc.call(JSON.stringify({
    function: func,
    params: params,
  }))
}

function handleIPC (event, f) {
  try {
    const res = f()
    event.returnValue = res
  } catch (error) {
    event.returnValue = { error: error }
  }
}

ipcMain.on('callDEX', (event, func, params) => {
  handleIPC(event, () => callDEX(func, params))
})

ipcMain.on('getPath', (event, name) => {
  handleIPC(event, () => JSON.stringify(app.getPath(name)))
})

ipcMain.on('cmdArgs', (event) => {
  handleIPC(event, () => JSON.stringify(process.argv))
})

function createWindow () {
  const win = new BrowserWindow({
    width: 800,
    height: 600,
    webPreferences: {
      nodeIntegration: true,
      contextIsolation: false
    }
  })

  if (process.argv.indexOf('--simnet') > -1) win.webContents.openDevTools()
  win.loadFile('index.html')
}

app.whenReady().then(createWindow)

app.on('window-all-closed', () => {
  if (process.platform !== 'darwin') {
    app.quit()
    callDEX('shutdown', '')
  }
})

app.on('activate', () => {
  if (BrowserWindow.getAllWindows().length === 0) {
    createWindow()
  }
})