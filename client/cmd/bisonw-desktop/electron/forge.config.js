const { FusesPlugin } = require('@electron-forge/plugin-fuses');
const { FuseV1Options, FuseVersion } = require('@electron/fuses');

module.exports = {
  packagerConfig: {
    name: 'Bison Wallet',
    appBundleId: 'com.bisonwallet.desktop',
    extraResource: ['../resources/'+require('./utils.js').getPlatform()],
    icon: '../src/bisonw.icns',
    appVersion: require('./package.json').version,
    executableName: 'Bison Wallet',
    asar: true,
    junk: true,
    platforms: ['darwin', 'win32', 'linux'],
    win32metadata: {
      CompanyName: 'The Decred Developers',
      FileDescription: 'A multi-coin wallet with DEX in-built.',
      OriginalFilename: 'BisonWallet.exe',
      ProductName: 'Bison Wallet',
      InternalName: 'Bison Wallet'
    },
    ignore: ['./node_modules', './resources', './installers'],
  },
  rebuildConfig: {},
  makers: [
    {
      name: '@electron-forge/maker-squirrel',
      config: {
        name: 'Bison Wallet',
        authors: 'The Decred Developers',
        description: 'A multi-coin wallet with DEX in-built.',
        setupIcon: '../src/favicon-32.ico',
        setupExe: 'BisonWallet.exe',
        noMsi: true,
      },
    },
    {
      name: '@electron-forge/maker-dmg',
      config: {
        format: 'ULFO',
        name: 'Bison Wallet',
        icon: '../src/bisonw.icns',
        overwrite: true,
      }
    },
    {
      name: '@electron-forge/maker-deb',
      config: {
        options: {
          maintainer: 'The Decred Developers',
          homepage: 'https://bisonwallet.com',
          icon: '../src/bisonw.png',
          categories: ['Office', 'Finance'],
          description: 'A multi-coin wallet with DEX in-built.',
        },
      },
    },
    {
      name: '@electron-forge/maker-rpm',
      config: {
        options: {
          maintainer: 'The Decred Developers',
          summary: 'A multi-coin wallet with DEX in-built.',
          icon: '../src/bisonw.png',
          categories: ['Office', 'Finance'],
        },
      },
    }
    // {
    //   name: '@electron-forge/maker-zip',
    //   platforms: ['darwin', 'win32', 'linux'],
    // }
  ],
  plugins: [
    {
      name: '@electron-forge/plugin-auto-unpack-natives',
      config: {},
    },
    // Fuses are used to enable/disable various Electron functionality
    // at package time, before code signing the application
    new FusesPlugin({
      version: FuseVersion.V1,
      [FuseV1Options.RunAsNode]: false,
      [FuseV1Options.EnableCookieEncryption]: true,
      [FuseV1Options.EnableNodeOptionsEnvironmentVariable]: false,
      [FuseV1Options.EnableNodeCliInspectArguments]: false,
      [FuseV1Options.EnableEmbeddedAsarIntegrityValidation]: true,
      [FuseV1Options.OnlyLoadAppFromAsar]: true,
    }),
  ],
};
