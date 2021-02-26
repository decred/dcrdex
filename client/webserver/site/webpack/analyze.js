const { merge } = require('webpack-merge')
const common = require('./common.js')
const BundleAnalyzerPlugin = require('webpack-bundle-analyzer').BundleAnalyzerPlugin

module.exports = merge(common, {
  mode: 'production',
  optimization: {
    usedExports: true,
    splitChunks: {
      chunks: 'all'
    }
  },
  plugins: [
    new BundleAnalyzerPlugin()
  ]
})
