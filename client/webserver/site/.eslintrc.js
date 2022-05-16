module.exports = {
  parser: '@typescript-eslint/parser',
  parserOptions: {
    project: './tsconfig.json', // Required to have rules that rely on Types.
    tsconfigRootDir: './'
  },
  extends: [
    'standard',
    'plugin:@typescript-eslint/recommended'
  ],
  env: {
      browser: true,
      node: true
  },
  plugins: [
    '@typescript-eslint' // Let's us override rules below.
  ],
  rules: {
    '@typescript-eslint/no-use-before-define': 'off',
    '@typescript-eslint/no-explicit-any': 'off',
    '@typescript-eslint/indent': 'off',
    'no-use-before-define': 'off',
    'no-trailing-spaces': 'error',
    'no-console': ['off'],
    'no-alert': 'error',
    'no-eval': 'error',
    'no-implied-eval': 'error',
    'object-shorthand': 'off'
  }
}
