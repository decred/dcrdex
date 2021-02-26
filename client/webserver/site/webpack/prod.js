const { merge } = require('webpack-merge')
const common = require('./common.js')
const CssMinimizerPlugin = require('css-minimizer-webpack-plugin')
const ESLintPlugin = require('eslint-webpack-plugin')

module.exports = merge(common, {
  mode: 'production',
  devtool: 'source-map',
  plugins: [new ESLintPlugin({
    formatter: 'stylish'
  })],
  optimization: {
    usedExports: true,
    minimize: true,
    minimizer: [
      `...`, // extend webpack 5's TerserPlugin
      new CssMinimizerPlugin({})
    ]
  },
  module: {
    rules: [
      {
        test: /\.js$/,
        exclude: /node_modules/,
        use: {
          loader: 'babel-loader',
          options: {
            presets: [
              [
                "@babel/preset-env",
                {
                  "exclude": ["@babel/plugin-transform-regenerator"]
                }
              ]
            ]
          }
        }
      }
    ]
  }
})
