package org.decred.dex.dexandroid;

import android.content.Context;
import android.content.SharedPreferences;

import com.google.gson.Gson;
import com.google.gson.reflect.TypeToken;

import java.lang.reflect.Type;
import java.util.ArrayList;
import java.util.List;

public class PreferenceManager {
    private static final String PREFERENCES_NAME = "settings";

    private static final String DEX_CLIENT_LIST_KEY = "dex_client_list";

    private final SharedPreferences sharedPreferences;
    private final Gson gson;

    public PreferenceManager(Context context) {
        sharedPreferences = context.getSharedPreferences(PREFERENCES_NAME, Context.MODE_PRIVATE);
        gson = new Gson();
    }

    public void saveDexClientList(List<DexClient> dexClientList) {
        SharedPreferences.Editor editor = sharedPreferences.edit();
        String json = gson.toJson(dexClientList);
        editor.putString(DEX_CLIENT_LIST_KEY, json);
        editor.apply();
    }

    public DexClient addDexClientFromURL(String url) {
        DexClient newItem = DexClient.newDexClientFromURL(url);
        List<DexClient> clientList = getDexClientList();
        clientList.add(newItem);
        saveDexClientList(clientList);
        return newItem;
    }

    public void removeDexClient(DexClient item) {
        List<DexClient> clientList = getDexClientList();
        clientList.remove(item);
        saveDexClientList(clientList);
    }

    public Boolean containsUrl(String url) {
        List<DexClient> clientList = getDexClientList();
        for (DexClient client : clientList) {
            if (client.url().equals(url)) {
                return true;
            }
        }
        return false;
    }

    public List<DexClient> getDexClientList() {
        String json = sharedPreferences.getString(DEX_CLIENT_LIST_KEY, "[]");
        Type type = new TypeToken<ArrayList<DexClient>>() {
        }.getType();
        return gson.fromJson(json, type);
    }
}