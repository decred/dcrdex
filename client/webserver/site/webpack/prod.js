const { merge } = require('webpack-merge')
const common = require('./common.js')
const CssMinimizerPlugin = require('css-minimizer-webpack-plugin')

module.exports = merge(common, {
  mode: 'production',
  devtool: 'source-map',
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
        test: /\.ts$/,
        exclude: /node_modules/,
        use: {
          // babel-loader does not fail on type errors. ts-loader does, but we
          // probably still want to transpile (right?).
          loader: 'babel-loader',
          options: {
            presets: [
              [
                "@babel/preset-typescript",
                {
                  "exclude": ["@babel/plugin-transform-typescript"]
                }
              ]
            ]
          }
        }
      }
    ]
  }
})
