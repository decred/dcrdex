const path = require('path')
const webpack = require('webpack')
const { merge } = require('webpack-merge')
const common = require('./common.js')

// the URL the development dex server.  This is proxied by the webpack dev server
const devUrl = 'http://127.0.0.3:5758'

module.exports = merge(common, {
  mode: 'development',
  module: {
    rules: [{
      test: /\.ts$/,
      use: 'ts-loader',
      exclude: /node_modules/,
    },
    {
      test: /\.s?[ac]ss$/,
      use: [
        {
          loader: 'style-loader'
        },
        {
          loader: 'css-loader',
          options: {
            modules: false,
            url: false,
            sourceMap: true
          }
        },
        {
          loader: 'sass-loader',
          options: {
            implementation: require('sass'), // dart-sass
            sourceMap: true
          }
        }
      ]
    }]
  },
  devtool: 'inline-source-map',
  plugins: [
    new webpack.HotModuleReplacementPlugin()
  ],
  entry: [
    // Runtime code for hot module replacement
    'webpack/hot/dev-server.js',
    // Dev server client for web socket transport, hot and live reload logic
    'webpack-dev-server/client/index.js?hot=true&live-reload=true',
    // Main entry point
    './src/index.ts'
  ],
  output: {
    filename: 'entry.js',
    path: path.resolve(__dirname, '../dist'),
    publicPath: '/js'
  },
  devServer: {
    port: 9000,
    devMiddleware: {
      index: false,
      publicPath: '/js'
    },
    webSocketServer: {
      type: 'ws',
      options: {
        path: '/wds'
      }
    },
    client: {
      webSocketURL: {
        pathname: '/wds'
      },
      reconnect: true,
      overlay: {
        errors: true,
        warnings: true,
        runtimeErrors: true
      },
      progress: true
    },
    proxy: [
      {
        ws: true,
        context: '/ws',
        target: devUrl,
        // Avoid crashing on "read ECONNRESET".
        onError: err => console.error(err)
      },
      {
        ws: false,
        context: () => true,
        target: devUrl,
        // Avoid crashing on "read ECONNRESET".
        onError: err => console.error(err)
      }
    ]
  },
  // Fixes weird issue with watch script. See
  // https://github.com/webpack/webpack/issues/2297#issuecomment-289291324
  watchOptions: {
    poll: true
  }
})
