The DEX client supports translations for the browser frontend and the notification messages from the backend.

To add a new locale, the translations must be defined in the following locations:

1. HTML strings (client/webserver/locales)
2. Notification strings (client/core/locale_ntfn.go)
3. JavaScript strings (client/webserver/site/src/js/locales.js)

If you decide to do the following for a different language, please see https://github.com/decred/dcrdex/wiki/Contribution-Guide for help with the github workflow.

## Step 1 - HTML

The HTML strings involved creating [client/webserver/locales/zh-cn.go](https://github.com/decred/dcrdex/blob/master/client/webserver/locales/zh-cn.go), which contains a `var ZhCN map[string]string` that provides a translation for a string identified by a certain key. The translation is not of the key itself, but of the corresponding English string in the `EnUS` map in https://github.com/decred/dcrdex/blob/master/client/webserver/locales/en-us.go. The name of this file should be a [BCP 47 language tag](https://www.w3.org/International/articles/bcp47/), preferably the "language-region" form.

The new HTML strings map must then be listed in https://github.com/decred/dcrdex/blob/master/client/webserver/locales/locales.go with an appropriate language tag.

Once the new file is created in **client/webserver/locales** and the new map registered in **client/webserver/locales/locales.go**, it is then necessary to run `go generate` in the **client/webserver/site** folder.  This creates the localized HTML template files in the **localized_html** folder.

If you modify any of the HTML templates in *client/webserver/site/src/html*, it
is also necessary to regenerate the localize templates with `go generate`.

## Step 2 - Notifications

The notification strings involved editing [client/core/locale_ntfn.go](https://github.com/decred/dcrdex/blob/master/client/core/locale_ntfn.go) with a new `var zhCN map[Topic]*translation`. These translations correspond to the English strings in the `enUS` map in the same file.

Note how in **client/core/locale_ntfn.go** there are "printf" specifiers like `%s` and `%d`.  These define the formatting for various runtime data, such as integer numbers, identifier strings, etc.  Because sentence structure varies between languages the order of those specifiers can be explicitly defined like `%[3]s` (the third argument given to printf in the code) instead of relying on the position of those specifiers in the formatting string.  This is necessary because the code that executes the printing always provides the values in a particular order, which may not be the same order in a translated string.

Once the translations are added to **client/core/locale_ntfn.go**, the new map is listed in the `var locales map[string]map[Topic]*translation` at the bottom of the same file.

## Step 3 - JavaScript

The JavaScript strings involved editing [client/webserver/site/src/js/locales.js](https://github.com/decred/dcrdex/blob/master/client/webserver/site/src/js/locales.js) with a new `export const zhCN` object, with strings corresponding to the English text in the `enUS` object at the top of the same file.  Finally, the new object is listed in the `const localesMap` at the end of the file.

When testing, remember to rebuild the site assets bundle with `npm ci && npm run build` in the **client/webserver/site** folder.