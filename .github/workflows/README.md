
# Build and Publish Snap Package workflow

This workflow builds a `snap` package and uploads it to the [Snap Store](https://snapcraft.io/store).  It has a `workflow_dispatch` trigger so it can be triggered manually only, it is not hooked up to any other GH event.

The store upload requires the `SNAPCRAFT_STORE_CREDENTIALS` variable to be set in [Github Secrets](https://github.com/decred/dcrdex/settings/secrets/actions).  
See https://github.com/snapcore/action-publish for details.

More information on Snapcraft authentication: [https://snapcraft.io/docs/snapcraft-authentication]