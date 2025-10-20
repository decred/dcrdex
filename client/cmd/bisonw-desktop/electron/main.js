const {
  app,
  BrowserWindow,
  Notification,
  ipcMain,
  Menu,
  Tray,
  dialog,
} = require("electron");
const { spawn } = require("node:child_process");
const path = require("node:path");
const { shell, nativeImage } = require("electron/common");
const { getPlatform } = require("./utils.js");

// defaultWebAddress is the local address where the bisonw web server listens.
const defaultWebAddress = "http://127.0.0.1:5758";

// save a reference to the Tray object globally to avoid garbage collection
let tray;
let bisonwProcess;
let isAppReady;
let network;
let isWebServerReady = false;
let isShuttingDown = false;
const activeOrdersLogoutErr = "cannot log out with active orders";

// "bisonw" is the binary that we will package.
const bisonwPath = path.resolve(path.join(getExtraFilesPath(), "./bisonw"));

// Spawn the bisonw process
bisonwProcess = spawn(bisonwPath);

bisonwProcess.stdout.on("data", async (data) => {
  const message = data.toString();
  if (isAppReady && message.includes("LOGIN AND TRADE")) {
    isWebServerReady = true;
    createWindow();
  }

  // Watch for logout messages and if there's an active order, warn the user via notification.
  if (message.includes(activeOrdersLogoutErr)) {
    new Notification({
      title: "Active Orders Detected",
      body: "You have active orders. Please cancel them before logging out to avoid failed swaps and account penalization.",
    }).show();
  }

  // Watch log and handle when user has an active order but wants to shut down.
  if (message.includes("Do you want to quit anyway?")) {
    dialog
      .showMessageBox(BrowserWindow.getFocusedWindow(), {
        type: "warning",
        buttons: ["Cancel", "Quit Anyway"],
        defaultId: 1,
        cancelId: 0,
        title: "Active Orders Detected",
        message:
          "You have active orders. Shutting down now may result in failed swaps and account penalization.",
        detail: "Do you still want to quit?",
      })
      .then((result) => {
        if (result.response === 1) {
          bisonwProcess.stdin.write("yes\n");
        } else {
          bisonwProcess.stdin.write("no\n");
          isShuttingDown = false; // user aborted
        }
      });
  }

  if (network === "") {
    if (message.includes("starting for network: mainnet")) network = "mainnet";
    else if (message.includes("starting for network: testnet"))
      network = "testnet";
    else if (message.includes("starting for network: simnet"))
      network = "simnet";
  }

  console.log(`${data}`);
});

bisonwProcess.on("exit", (code) => {
  console.log(`Bison Wallet process exited with code ${code}`);

  if (isShuttingDown) {
    app.exit(code || 0);
  }
});

bisonwProcess.stderr.on("data", (data) => {
  console.error(data.toString());

  if (!isShuttingDown) {
    isShuttingDown = true;
    console.log("Bison Wallet process reported an error. Shutting down...");
    bisonwProcess.kill();
  }
});

app.whenReady().then(() => {
  isAppReady = true;

  app.on("activate", () => {
    if (BrowserWindow.getAllWindows().length === 0) createWindow();
  });

  setApplicationMenu();
  setTrayMenu();
  setDockMenu();
});

// IPC listener for notifications from rendered web pages.
ipcMain.on("notify", (_, { title, body }) => {
  new Notification({ title, body }).show();
});

ipcMain.on("openUrl", (_, url) => {
  shell.openExternal(url)
});

// Quit when all windows are closed, except on macOS.
app.on("window-all-closed", () => {
  if (getPlatform() !== "mac") app.quit();
});

app.on("before-quit", (e) => {
  if (bisonwProcess && !isShuttingDown) {
    e.preventDefault();
    isShuttingDown = true;
    bisonwProcess.kill("SIGINT");
  }
});

function createWindow() {
  if (!isWebServerReady) return;

  const win = new BrowserWindow({
    width: 1000,
    height: 700,
    show: false,
    backgroundColor: "rgb(5, 16, 27)",
    webPreferences: {
      preload: __dirname + "/preload.js",
    },
  });

  win.once("ready-to-show", () => win.show());

  win.loadURL(defaultWebAddress);
}

function setTrayMenu() {
  const bisonwIcon = path.resolve(
    path.join(getExtraFilesPath(), "./bisonw-16.png")
  );
  const icon = nativeImage.createFromPath(bisonwIcon);
  tray = new Tray(icon);
  const trayTemplate = Menu.buildFromTemplate([
    {
      label: "Bison Wallet is running",
      enabled: false,
    },
    {
      label: "Quit Bison Wallet",
      accelerator: "CmdOrCtrl+Q",
      click: () => {
        app.quit();
      },
    },
  ]);
  tray.setContextMenu(trayTemplate);
}

function setDockMenu() {
  if (process.platform !== "darwin") return;
  const dockMenu = Menu.buildFromTemplate([
    {
      label: "New Window",
      click: createWindow(),
    },
    {
      label: "Open Logs",
      click: openLog,
    },
  ]);
  app.dock.setMenu(dockMenu);
}

function setApplicationMenu() {
  const isMac = process.platform === "darwin";
  const template = [
    { role: "appMenu" },
    {
      label: "File",
      submenu: [isMac ? { role: "close" } : { role: "quit" }],
    },
    {
      label: "Edit",
      submenu: [
        { role: "undo" },
        { role: "redo" },
        { type: "separator" },
        { role: "cut" },
        { role: "copy" },
        { role: "paste" },
        ...(isMac
          ? [
              { role: "pasteAndMatchStyle" },
              { role: "delete" },
              { role: "selectAll" },
              { type: "separator" },
              {
                label: "Speech",
                submenu: [{ role: "startSpeaking" }, { role: "stopSpeaking" }],
              },
            ]
          : [{ role: "delete" }, { type: "separator" }, { role: "selectAll" }]),
      ],
    },
    { role: "viewMenu" },
    {
      label: "Window",
      submenu: [
        {
          label: "New Window",
          accelerator: "CmdOrCtrl+W",
          click: () => {
            createWindow();
          },
        },
        { role: "minimize" },
        { role: "zoom" },
        ...(isMac
          ? [
              { type: "separator" },
              { role: "front" },
              { type: "separator" },
              { role: "window" },
            ]
          : [{ role: "close" }]),
      ],
    },
    {
      label: "Others",
      submenu: [
        {
          label: "Open Logs",
          accelerator: isMac ? "Cmd+Alt+L" : "Ctrl+Shift+L",
          click: openLog,
        },
      ],
    },
    {
      role: "help",
      submenu: [
        {
          label: "Website",
          click: async () => {
            const { shell } = require("electron");
            await shell.openExternal("https://bisonwallet.com");
          },
        },
        {
          label: "Support Channel",
          click: async () => {
            const { shell } = require("electron");
            await shell.openExternal("https://matrix.to/#/#support:decred.org");
          },
        },
        {
          label: "Documentation",
          click: async () => {
            const { shell } = require("electron");
            await shell.openExternal("https://github.com/decred/dcrdex/wiki");
          },
        },
      ],
    },
  ];

  const menu = Menu.buildFromTemplate(template);
  Menu.setApplicationMenu(menu);
}

// openLog opens the log directory in the system file explorer.
function openLog() {
  const logPath = path.join(
    app.getPath("appData"),
    "Dexc",
    network || "mainnet",
    "logs"
  );
  shell.openPath(logPath);
}

// getExtraFilesPath is a helper to get the path to the extra files (binaries & icon).
function getExtraFilesPath() {
  const IS_DEV = process.env.NODE_ENV === "development";
  const { isPackaged } = app;

  const extraFilesPath =
    !IS_DEV && isPackaged
      ? path.join(process.resourcesPath, getPlatform())
      : path.join(app.getAppPath(), "..", "resources", getPlatform());

  return extraFilesPath;
}
