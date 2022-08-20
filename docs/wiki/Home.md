# <img src="images/logo_wide_v1.svg" alt="DCRDEX" width="250">

The dcrdex wiki provides documentation for dcrdex users and developers.

Also, have a look at the [README](https://github.com/decred/dcrdex/blob/master/README.md) and the [spec](https://github.com/decred/dcrdex/blob/master/spec/README.mediawiki).

## Usage

To get started using DCRDEX, see the [Client Installation and Configuration](https://github.com/decred/dcrdex/wiki/Client-Installation-and-Configuration) page.

## Contribute changes to the wiki

The following instructions assume that you have already forked **dcrdex**.

1. Initialize your forked wiki on github by navigating to the wiki tab of your forked **dcrdex** repo and clicking the "Create the first page" button. Be sure to "Save page" as well.

    <img src="images/wiki-creation.png" width="700">

2. In your terminal, navigate to your local **dcrdex** directory and add your remote wiki.

    ```sh
    git remote add wiki https://github.com/your-username/dcrdex.wiki.git
    git push wiki -d master
    git subtree push --prefix docs/wiki wiki master
    ```

3. Now after committing changes to the files in the docs/wiki folder, you can apply them to your dcrdex wiki and view them immediately. Assuming you are in the root directory:

    ```sh
    git push wiki $(git subtree split --prefix docs/wiki):master --force
    ```

4. Make a pull request with your changes to dcrdex. They will make it to the main wiki eventually.
