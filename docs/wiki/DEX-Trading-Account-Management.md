# Guide to Managing Your DEX Trading Account

In this guide, we will discuss how you can manage your DEX Trading Account.

Let's get started!

## Setting Up Your DEX Trading Account

Before you can start buying and selling on Decred DEX, you'll need a DEX trading
account. If you've already followed all the steps in the [Initial Setup
Guide](./Client-Installation-and-Configuration.md/#initial-setup), you're all
set. If not, let's create your DEX trading account together:

1. Go to the `Markets` Page.
2. Find the `Create Account` button and click it.

    <img src="./images/create-account-market-page.png" width="300">

3. Follow the steps in the [Initial Setup Guide](./Client-Installation-and-Configuration.md/#initial-setup) from step 4.

**IF** you've already created the wallet you plan to use and funded it with the
required bond amount, you can skip steps 6 and 7 of the [Initial Setup
Guide](./Client-Installation-and-Configuration.md/#initial-setup).

## Adding an existing DEX Trading Account

If you have an existing DEX account that you want to use with a new setup, here's how you can do it:

1. Go to the `Settings` Page.
2. Click on `Import Account`.

    <img src="./images/dex-accounts-settings.png" width="300">

3. Choose the exported DEX account `.json` file by clicking on `load from file`.

    <img src="./images/import-dex-from-file.png" width="300">

4. Click `Authorize Import`.

Easy as that!

## Disabling a DEX Trading Account

If you want to temporarily disable your DEX trading account, follow these steps:

1. Visit the `Settings` Page.
2. Click on the DEX account in the `Registered Dexes` list. For example, it might be `dex.decred.org:7232 ⚙️`.

    <img src="./images/dex-accounts-settings.png" width="300">

2. On the selected DEX account settings page, click on the `Disable Account`.
3. Confirm the action with your app password.

If successful, the DEX trading account **will not be listed until it is added again**.

**Note**: Keep in mind that you can't disable your account if you have active
orders or unspent bonds.

## Re-enabling a DEX Trading Account

If you disabled your account but want to re-enable it:

1. Go to the `Settings` Page. 
2. Choose the DEX host you previously disabled. You can click on a pre-defined
   host or enter the address of the host after clicking on the `add a different
   server` button.

    <img src="images/add-dex-reg.png" width="320">

3. Your old account will be automatically discovered and enabled. 

**Note**: Remember, after re-enabling, you'll need to create fidelity bonds to
use your account again.

## Managing your DEX Trading Account Tier

To manage your DEX trading account tier, here's what you need to do:

1. Visit the `Settings` Page.
2. Click on the DEX account in the `Registered Dexes` list. In this example, it
   will be `dex.decred.org:7232 ⚙️`

    <img src="./images/dex-accounts-settings.png" width="300">

3. On the DEX account settings page, click `Update Bond Options`.
4. Choose the asset for your fidelity bonds and set your `Target Tier`.
5. Submit the form to update your bond options.

**Note**: Make sure you have enough funds to cover your desired `Target Tier`.

## Wrapping Up

That's it! You've learned how to manage your DEX Trading Account.

While this guide covers the basics, there are more advanced topics like updating
DEX hosts and TLS certificates. But for now, you're ready to dive into the
exciting world of DEX trading!

## Glossary

- **Fidelity Bonds**: These are locked funds redeemable by you in the future.
They help prevent disruptive behavior in trades like backing out on swaps.
- **Target Tier**: This is the target account tier you wish to maintain.
Set to zero if you wish to disable tier maintenance (i.e do not post new bonds).