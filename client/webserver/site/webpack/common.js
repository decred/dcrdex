const path = require('path')
const { CleanWebpackPlugin } = require('clean-webpack-plugin')
const MiniCssExtractPlugin = require('mini-css-extract-plugin')
const StyleLintPlugin = require('stylelint-webpack-plugin')

module.exports = {
  entry: {
    app: './src/index.js'
  },
  optimization: {
    splitChunks: {
      chunks: 'all'
    }
  },
  module: {
    rules: [
      {
        test: /\.s?[ac]ss$/,
        use: [
          MiniCssExtractPlugin.loader,
          {
            loader: 'css-loader',
            options: {
              modules: false, url: false, sourceMap: true
            }
          },
          {
            loader: 'sass-loader',
            options: {
              sourceMap: true
            }
          }
        ]
      }
    ]
  },
  plugins: [
    new CleanWebpackPlugin(),
    new MiniCssExtractPlugin({
      filename: '../dist/style.css'
    }),
    new StyleLintPlugin()
  ],
  output: {
    filename: 'entry.js',
    path: path.resolve(__dirname, '../dist'),
    publicPath: '/dist/'
  },
  // Fixes weird issue with watch script. See
  // https://github.com/webpack/webpack/issues/2297#issuecomment-289291324
  watchOptions: {
    poll: true
  }
}
