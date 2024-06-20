package org.decred.dex.dexandroid;

import java.io.Serializable;
import java.net.MalformedURLException;
import java.net.URL;

// DexClient is a model representing a DEX host the application can connect to.
public record DexClient(String url, String name) implements Serializable {

    public static DexClient newDexClientFromURL(String url) {
        String name = DexClient.convertUrlToName(url);
        return new DexClient(url, name);
    }

    @Override
    public boolean equals(Object obj) {
        if (this == obj) {
            return true;
        }
        if (obj == null || getClass() != obj.getClass()) {
            return false;
        }
        DexClient dexClient = (DexClient) obj;
        return url.equals(dexClient.url);
    }

    private static String convertUrlToName(String url) {
        try {
            URL urlObj = new URL(url);
            return urlObj.getHost();
        } catch (MalformedURLException e) {
            return url;
        }
    }
}
