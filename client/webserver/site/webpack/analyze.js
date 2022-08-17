const { merge } = require('webpack-merge')
const common = require('./common.js')
const BundleAnalyzerPlugin = require('webpack-bundle-analyzer').BundleAnalyzerPlugin

module.exports = merge(common, {
  mode: 'production',
  module: {
    rules: [{
      test: /\.ts$/,
      use: 'ts-loader',
      exclude: /node_modules/,
    }]
  },
  optimization: {
    usedExports: true
  },
  plugins: [
    new BundleAnalyzerPlugin()
  ]
})
