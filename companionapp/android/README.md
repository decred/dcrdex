
## Android DEX client

This is a companion app which can be used to connect to a DEX client (running on e.g. a desktop computer).  It is not a standalone DEX client in itself.

More details to follow.

## How to Build

### Using Android Studio

Please see: https://raw.githubusercontent.com/guardianproject/tor-android/master/BUILD

### Using Docker

The APK can also be built using a self-contained Docker image: https://github.com/mingchen/docker-android-build-box

To build an unsigned debug APK, run:

```
cd companionapp/android
docker pull mingc/android-build-box:latest
docker run --rm -v `pwd`:/project mingc/android-build-box bash -c 'cd /project; ./gradlew assembleDebug'
```

The APK will be placed in `companionapp/android/app/build/outputs/apk/debug/app-debug.apk`.  This can then be installed on an Android device using `adb install`.

### Github workflow

Run the *Android build* workflow in Github Actions.  The resulting debug APK artifacts will be downloadable after the workflow is complete.

## Release Build

A signed release APK can be built using `assembleRelease`. This requires a
keystore and signing credentials.

### 1. Create a keystore (one-time)

```
keytool -genkey -v -keystore keystore.jks -keyalg RSA -keysize 2048 -validity 10000 -alias your-alias
```

Place the resulting `keystore.jks` file in `companionapp/android/`.

### 2. Set environment variables

The build reads signing credentials from environment variables:

```
export RELEASE_KEYSTORE_PASSWORD="your-keystore-password"
export RELEASE_KEYSTORE_ALIAS="your-alias"
export RELEASE_KEY_PASSWORD="your-key-password"
```

### 3. Build

```
cd companionapp/android
./gradlew assembleRelease
```

The signed APK will be placed in
`companionapp/android/app/build/outputs/apk/release/app-release.apk`.

### CI release builds

To build releases in GitHub Actions, add `RELEASE_KEYSTORE_PASSWORD`,
`RELEASE_KEYSTORE_ALIAS`, and `RELEASE_KEY_PASSWORD` as repository secrets,
and upload `keystore.jks` as a secret file. Then update the workflow to run
`assembleRelease` and provide the secrets as environment variables.
