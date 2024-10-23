package org.decred.dex.dexandroid;

import android.app.Application;

// DexCompanionApp overrides Application.  It is responsible for creating and
// storing objects app-wide objects such as GeckoViewHelper and Tor.
public class DexCompanionApp extends Application {
    private GeckoViewHelper geckoViewHelper;

    @Override
    public void onCreate() {
        super.onCreate();
        geckoViewHelper = new GeckoViewHelper();
    }

    public GeckoViewHelper getGeckoViewHelper() {
        return geckoViewHelper;
    }

    public static final String LOG_TAG = "DCRDEX";
}
