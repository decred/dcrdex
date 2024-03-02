const path = require('path')
const webpack = require('webpack')
const { CleanWebpackPlugin } = require('clean-webpack-plugin')
const StyleLintPlugin = require('stylelint-webpack-plugin')
const ESLintPlugin = require('eslint-webpack-plugin')

const child_process = require('child_process')
function git(command) {
  return child_process.execSync(`git ${command}`, { encoding: 'utf8' }).trim();
}

module.exports = {
  target: "web",
  plugins: [
    new webpack.EnvironmentPlugin({
      COMMITHASH: git('rev-parse HEAD'),
    }),
    new CleanWebpackPlugin(),
    new StyleLintPlugin({
      threads: true,
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
    extensions: ['.ts', ".js"],
  }
}
