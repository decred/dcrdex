const { merge } = require('webpack-merge')
const common = require('./common.js')
const ESLintPlugin = require('eslint-webpack-plugin')
const path = require('path')

module.exports = merge(common, {
  mode: 'development',
  plugins: [new ESLintPlugin({
    formatter: 'stylish'
  })],
  devtool: 'inline-source-map'
})
