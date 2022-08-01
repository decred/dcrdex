<img src="images/logo_wide_v1.svg" width="250">

The dcrdex wiki provides documentation for dcrdex users and developers. 

Most likely you want to look at the [README](https://github.com/decred/dcrdex/blob/master/README.md) or the [spec](https://github.com/decred/dcrdex/blob/master/spec/README.mediawiki) instead of these wiki pages.

## Contribute changes to the wiki

The following instructions assume that you have already forked **dcrdex**.

1. Initialize your forked wiki on github by navigating to the wiki tab of your forked **dcrdex** repo and clicking the "Create the first page" button.

<img src="images/wiki-creation.png" width="700">

2. In your terminal, navigate to your local **dcrdex** directory and clone the wiki from the **decred/dcrdex** repo.

`git clone https://github.com/decred/dcrdex.wiki.git wiki`

3. Create a remote for your fork's wiki, replacing 'your-username' with your username.

`cd wiki; git remote add me https://github.com/your-username/dcrdex.wiki.git`

4. Create a branch to track the decred/dcrdex wiki.

`git checkout -b master-upstream origin/master`

5. Now when you want to do some work, create a new branch based on this one.

`git checkout master-upstream`

`git pull`

`git checkout -b cool-stuff`

6. Unfortunately, you cannot view any wiki branch but master on github, so you'll want to commit your changes to your fork's master wiki branch and view them in your repo at github before requesting a merge.

`git push --force me cool-stuff:master`

7. Also unfortunately, github does not support pull requests for wikis, so contact @chappjc, and tell him you'd like to merge changes from your forked repo wiki. Keep your wiki master branch updated with your changes as you make them so that they can be viewed during review.
