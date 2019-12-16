const merge = require('webpack-merge')
const common = require('./common.js')

module.exports = merge(common, {
  mode: 'development',
  devtool: 'inline-source-map',
  devServer: {
    contentBase: './public/index.js',
    port: 54321
  },
  module: {
    rules: [
      {
        test: /\.js$/,
        exclude: /node_modules/,
        loader: 'eslint-loader',
        options: {
          formatter: require('eslint/lib/formatters/stylish')
        }
      }
    ]
  }
})
