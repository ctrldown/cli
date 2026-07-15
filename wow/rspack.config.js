const rspack = require('@rspack/core');
const path = require('path');

/**
 * @type {import('@rspack/cli').Configuration}
 */
module.exports = {
  entry: './src/index.ts',
  output: {
    // 'auto' allows the remote to be deployed to any centralized asset gateway (like MinIO or CDN).
    // It dynamically determines the path based on where remoteEntry.js is hosted, 
    // ensuring asset paths don't break when requested by the Shell App.
    publicPath: 'auto',
    path: path.resolve(__dirname, 'dist'),
    clean: true,
  },
  resolve: {
    extensions: ['.tsx', '.ts', '.jsx', '.js'],
  },
  module: {
    rules: [
      {
        test: /\.(ts|tsx)$/,
        exclude: [/node_modules/],
        loader: 'builtin:swc-loader',
        options: {
          jsc: {
            parser: {
              syntax: 'typescript',
              tsx: true,
            },
            transform: {
              react: {
                runtime: 'automatic',
              },
            },
          },
        },
      },
    ],
  },
  plugins: [
    new rspack.container.ModuleFederationPlugin({
      name: 'baseReactApp',
      filename: 'remoteEntry.js',
      exposes: {
        './App': './src/App.tsx',
        './store': './src/store/index.ts',
      },
      shared: {
        react: { singleton: true, requiredVersion: '^18.2.0' },
        'react-dom': { singleton: true, requiredVersion: '^18.2.0' },
        'nanostores': { singleton: true },
        '@nanostores/react': { singleton: true }
      },
    }),
  ],
};
