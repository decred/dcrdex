import neostandard from 'neostandard'

export default [
  { ignores: ['dist/', 'node_modules/'] },

  ...neostandard({ ts: true, filesTs: ['**/*.ts', '**/*.tsx'] }),

  {
    rules: {
      '@typescript-eslint/no-use-before-define': 'off',
      '@typescript-eslint/no-explicit-any': 'off',
      'no-use-before-define': 'off',
      'no-console': 'off',
      'no-alert': 'error',
      'no-eval': 'error',
      'no-implied-eval': 'error',
      'object-shorthand': 'off',

      // Stylistic overrides (moved from core eslint to @stylistic)
      '@stylistic/no-trailing-spaces': 'error',
      '@stylistic/quote-props': ['error', 'consistent'],
      '@stylistic/object-property-newline': 'off',
      '@stylistic/indent': 'off',
      '@stylistic/block-spacing': 'off',
      '@stylistic/object-curly-spacing': 'off',
      '@stylistic/key-spacing': 'off',
      '@stylistic/semi': 'off',
      '@stylistic/space-infix-ops': 'off',

      // JSX stylistic — preserve existing code style
      '@stylistic/jsx-quotes': 'off',
      '@stylistic/jsx-indent': 'off',
      '@stylistic/jsx-indent-props': 'off',
      '@stylistic/jsx-curly-spacing': 'off',
      '@stylistic/jsx-closing-tag-location': 'off',
      '@stylistic/jsx-closing-bracket-location': 'off',
      '@stylistic/jsx-curly-newline': 'off',
      '@stylistic/jsx-wrap-multilines': 'off',
      '@stylistic/jsx-tag-spacing': 'off',

      // React rules — preserve existing code style
      'react/self-closing-comp': 'off',
      'react/jsx-boolean-value': 'off',
      'react/jsx-handler-names': 'off'
    }
  }
]
