<a id="top"/>

## Prerequisites

Before contributing to the dcrdex wiki, ensure you have the following:

- A GitHub account.
- Basic knowledge of Markdown syntax.
- [Markdownlint-cli2 linter](https://github.com/DavidAnson/markdownlint-cli2-action) installed for checking markdown files.

    You can install it via npm (install [Node.js](https://nodejs.org/en/download/) to use npm):

    ```sh
    npm install -g markdownlint-cli2
    ```

## Steps to Contribute

1. Fork the dcrdex repo.

2. Edit files in the `docs/wiki` directory. Use Markdown syntax for formatting. Run the markdown linter to ensure compliance with style guidelines.

    ```sh
    markdownlint-cli2 -config .markdownlint.json ./docs/wiki/<Your-Edited-File>.md
    ```

    Due to the `back to top` feature on the doc pages, you should ignore this error:

    ```txt
    docs/wiki/<File-Name>.md:1 MD041/first-line-heading/first-line-h1 First line in a file should be a top-level heading [Context: "<a id="top"/>"]
    ```

3. Submit a PR.

Your changes will be reviewed. Updates may be requested before merging.

---

[⤴ Back to Top](#top)
