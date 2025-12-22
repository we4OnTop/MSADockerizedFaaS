const {merge} = require('webpack-merge');
const common = require('./webpack.common.js');
const HtmlWebpackPlugin = require('html-webpack-plugin');
const CopyPlugin = require('copy-webpack-plugin');

module.exports = merge(common, {
  mode: 'production',
  entry: {
    app: './js/app.js',
  },
  output: {
    clean: true,
    // ...other output options
  },
  plugins: [
    new HtmlWebpackPlugin({
      template: './index.html',
      inject: false
    }),
    new CopyPlugin({
      patterns: [
        {from: 'public', to: 'public'},
        {from: 'css', to: 'css'},
      ],
    }),
  ],
  module: {
    rules: [
      {
        test: /\.js$/,
        loader: 'string-replace-loader',
        options: {
          search: /document\.(getElementById|querySelector|querySelectorAll|getElementsByClassName|getElementsByTagName|getElementsByName)/g,
          replace: 'root.$1'
        }
      },
      {
        test: /\.js$/,
        loader: 'string-replace-loader',
        options: {
          search: /\/\/DO NOT REMOVE -> USED IN BUILD PROCESS! !  ! !/g,
          replace: `document.addEventListener('shadowDOMReady', (event) => {
  const root = event.detail.shadowRoot;`,
        }
      },
      {
        test: /\.js$/,
        loader: 'string-replace-loader',
        options: {
          search: /\/\/DO NOT REMOVE -> USED FOR BUILD PROCESS !!! ! ! !!/g,
          replace: `});`,
        }
      },
      {
        test: /\.css$/,
        enforce: "pre",
        use: [
          "style-loader",
          "css-loader",
          {
            loader: "string-replace-loader",
            options: {
              // Replace :root or :host with .root
              search: /:(root|host)/g,
              replace: '.root'
            }
          }
        ]
      }

    ]
  }
});

