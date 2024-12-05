const path = require('path')
const webpack = require('webpack')
const { CleanWebpackPlugin } = require('clean-webpack-plugin')
const MiniCssExtractPlugin = require('mini-css-extract-plugin')
const StyleLintPlugin = require('stylelint-webpack-plugin')
const ESLintPlugin = require('eslint-webpack-plugin')

const fs = require('node:fs')
const buildIdFilename = 'webpack-build-id.txt'

function randBuildId () {
  const buildID = JSON.stringify(Math.floor(Math.random() * 1000000000)).trim()
  console.log('WEBPACK_BUILD_ID:', buildID)
  fs.writeFile(buildIdFilename, buildID, err => {
    if (err) {
      console.error(err)
    } else {
      console.log(' ', buildID, ' written to ', buildIdFilename)
    }
  })
  return buildID
}

module.exports = {
  target: 'web',
  module: {
    rules: [
      {
        test: /\.s?[ac]ss$/,
        use: [
          MiniCssExtractPlugin.loader,
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
      }
    ]
  },
  plugins: [
    new webpack.DefinePlugin({
      'process.env.WEBPACK_BUILD_ID': randBuildId()
    }),
    new CleanWebpackPlugin(),
    new MiniCssExtractPlugin({
      filename: '../dist/style.css'
    }),
    new StyleLintPlugin({
      threads: true
    }),
    new ESLintPlugin({
      extensions: ['ts'],
      formatter: 'stylish'
    })
  ],
  output: {
    filename: 'entry.js',
    path: path.resolve(__dirname, '../dist'),
    publicPath: '/dist/'
  },
  resolve: {
    extensions: ['.ts', '.js']
  },
  // Fixes weird issue with watch script. See
  // https://github.com/webpack/webpack/issues/2297#issuecomment-289291324
  watchOptions: {
    poll: true
  }
}
