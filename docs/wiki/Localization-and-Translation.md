# Localization and Translation

Bison Wallet supports translations for the browser frontend and the notification messages from the backend.

To add a new locale, the translations must be defined in the following locations:

1. HTML strings (client/webserver/locales)
2. Notification strings (client/core/locale_ntfn.go)
3. JavaScript strings (client/webserver/site/src/js/locales.ts)

If you decide to do the following for a different language, please see the [Contribution Guide](https://github.com/decred/dcrdex/wiki/Contribution-Guide) for help with the github workflow.

## Step 1 - HTML

To create or update the HTML translations, create or modify the appropriate dictionary
in the `client/webserver/locales` directory. These dictionaries map HTML template
keys to translations. New or modified entries should use the translation in the English
dictionary (`var EnUS` in the file `en-us.go`) as the source text. The goal is to duplicate
entries for all keys in the English dictionary.

When creating a dictionary for a new language, use the BCP 47 language tag to construct
the file's name. Then new language's HTML strings map must then be listed in [client/webserver/locales/locales.go](https://github.com/decred/dcrdex/blob/master/client/webserver/locales/locales.go) with an appropriate language tag.

## Step 2 - Notifications

To update the notification translations, add or modify the translation in the appropriate locale dictionary map (e.g `originLocale`, `ptBR`, etc) in the [`client/core/locale_ntfn.go`](https://github.com/decred/dcrdex/blob/master/client/core/locale_ntfn.go) file. These dictionaries maps notification keys to translations. New or modified entries should use the translation in the English dictionary (`var originLocale map[Topic]*translation` in the file `locale_ntfn.go`) as the source text. The goal is to duplicate entries for all keys in the English dictionary.

When creating a dictionary for a new language, use the BCP 47 language tag to construct the map name and it's translations should correspond to the English strings in the `originLocale` map in the same file.

Note how in **client/core/locale_ntfn.go** there are "printf" specifiers like `%s` and `%d`.  These define the formatting for various runtime data, such as integer numbers, identifier strings, etc.  Because sentence structure varies between languages the order of those specifiers can be explicitly defined like `%[3]s` (the third argument given to printf in the code) instead of relying on the position of those specifiers in the formatting string.  This is necessary because the code that executes the printing always provides the values in a particular order, which may not be the same order in a translated string.

Once the translations are added to **client/core/locale_ntfn.go**, the new map is listed in the `var locales map[string]map[Topic]*translation` at the bottom of the same file.

## Step 3 - JavaScript

To update the JavaScript strings translation in [client/webserver/site/src/js/locales.ts](https://github.com/decred/dcrdex/blob/master/client/webserver/site/src/js/locales.ts), add or modify the translation in the appropriate language object.

When creating a dictionary for a new language, create the language dictionary object (e.g `export const ar: Locale = { ... }`) then add strings translation corresponding to the English text in the `enUS` object at the top of the same file.  Finally, the new object should be listed in the `const localesMap` at the end of the file.

When testing, remember to rebuild the site assets bundle with `npm ci && npm run build` and bump the cache with `./bust_caches.sh` in the **client/webserver/site** folder.
