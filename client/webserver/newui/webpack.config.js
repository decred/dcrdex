const path = require('path');
const HtmlWebpackPlugin = require('html-webpack-plugin');

module.exports = {
    entry: './src/index.tsx',
    devtool: 'inline-source-map',
    module: {
        rules: [
            {
                test: /\.tsx?$/,
                use: 'ts-loader',
                exclude: /node_modules/
            },
            {
                test: /\.s[ac]ss$/i,
                use: [
                    // Creates `style` nodes from JS strings
                    "style-loader",
                    // Translates CSS into CommonJS
                    {
                        loader: "css-loader",
                        options: {
                            url: false
                        }
                    },
                    // Compiles Sass to CSS
                    {
                        loader: "sass-loader",
                        options: {
                            api: "modern",
                            implementation: require("sass")
                        },
                    },
                ],
            },
            {
                test: /\.(woff2?|png|svg|jpe?g|gif)$/i,
                type: 'asset/resource',
                generator: {
                    emit: false
                }
            },
        ],
    },
    resolve: {
        extensions: ['.tsx', '.ts', '.js']
    },
    output: {
        filename: 'bundle.js',
        path: path.resolve(__dirname, 'dist'),
        publicPath: '/js/'
    },
    plugins: [
        new HtmlWebpackPlugin({
            template: 'src/index.html',
            inject: false
        }),
    ],
};
