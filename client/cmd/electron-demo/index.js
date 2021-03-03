const { ipcRenderer } = require('electron')
const path = require('path')

Mainnet = 0
Testnet = 1
Simnet = 2

NetName = {
    [Mainnet]: Mainnet,
    [Testnet]: Testnet,
    [Simnet]: Simnet
}

LogLevelTrace = 0
LogLevelDebug = 1
LogLevelInfo = 2
LogLevelWarn = 3
LogLevelError = 4
LogLevelCritical = 5
LogLevelOff = 6

DCR_ID = 42
BTC_ID = 0

function appGetPath (name) {
    return fetchIPC('getPath', name)
}

function fetchIPC (func, ...args) {
    let res = ipcRenderer.sendSync(func, ...args)
    res = res ? JSON.parse(res) : null
    if (res && res.error) throw Error(String(res.error))
    return res
}

class DEX {
    static startCore () {
        const dbPath = path.join(appGetPath('temp'), (new Date().getTime()).toString(), 'dexc', 'db.db')
        return fetchIPC('callDEX', 'startCore', {
            dbPath: dbPath,
            net: Simnet,
            logLevel: LogLevelDebug,
        })
    }

    static isInitialized () {
        const res = fetchIPC('callDEX', 'IsInitialized', '')
        console.log("isInitialized res", res, typeof res)
        return res
    }

    static init (pw) {
        return fetchIPC('callDEX', 'Init', { pass: pw })
    }

    static startServer (addr) {
        return fetchIPC('callDEX', 'startServer', addr)
    }

    static createWallet (assetID, config, pass, appPass) {
        return fetchIPC('callDEX', 'CreateWallet', {
            assetID: assetID,
            config: config,
            pass: pass,
            appPass: appPass,
        })
    }

    static register (addr, appPass, fee, cert) {
        return fetchIPC('callDEX', 'Register', {
            url: addr,
            appPass: appPass,
            fee: fee,
            cert: cert,
        })
    }
    
    static user () {
        return fetchIPC('callDEX', 'User', '')
    }
}

/* sleep can be used by async functions to pause for a specified period. */
function sleep (ms) {
return new Promise(resolve => setTimeout(resolve, ms))
}

const mainDiv = document.getElementById('main')

async function writeMain (s) {
    const div = document.createElement('div')
    div.textContent = s
    mainDiv.appendChild(div)
}

function stringToUTF8Hex (s) {
    return s.split("").map(c => c.charCodeAt(0).toString(16).padStart(2, "0")).join("")
}

async function prepSimnet () {

    const pw = "abc"
    if (!DEX.isInitialized()) {
        await writeMain('Initializing DEX')
        await writeMain(`result: ${DEX.init(pw)}`)
    }

    const homeDir = appGetPath('home')
    const dextestDir = path.join(homeDir, 'dextest')

    const user = DEX.user()
    if (!user.assets[DCR_ID].wallet) {
        const walletCertPath = path.join(dextestDir, 'dcr', 'alpha', 'rpc.cert')
        await writeMain('Loading simnet Decred wallet')
        DEX.createWallet(DCR_ID, {
            account: 'default',
            username: 'user',
            password: 'pass',
            rpccert: walletCertPath,
            rpclisten: '127.0.0.1:19567'
        }, pw, pw)

        await writeMain('Loading simnet Bitcoin wallet')
        DEX.createWallet(BTC_ID, {
            walletname: '', // alpha wallet
            rpcuser: 'user',
            rpcpassword: 'pass',
            rpcport: '20556',
        }, pw, pw)
    }

    const simnetDexAddr = '127.0.0.1:17273'
    if (!user.exchanges[simnetDexAddr]) {
        await writeMain('Registering at simnet DEX server')
        const defaultRegFee = 1e8
        const serverCertPath = path.join(dextestDir, 'dcrdex', 'rpc.cert')
        DEX.register(simnetDexAddr, pw, defaultRegFee, serverCertPath)
    }
}

async function run () {
    let net = Mainnet
    const args = fetchIPC('cmdArgs', '')
    if (args.indexOf('--simnet') > -1) net = Simnet
    else if (args.indexOf('--testnet') > -1) net = Simnet

    console.log("using network", NetName[net])

    await writeMain('Starting DEX Core')
    DEX.startCore()

    if (net == Simnet) prepSimnet()

    // Start server
    const addr = `localhost:54321`
    await writeMain('Starting Web Server')
    await writeMain(`result: ${DEX.startServer(addr)}`)

    window.location.href = `http://${addr}`
}

run()