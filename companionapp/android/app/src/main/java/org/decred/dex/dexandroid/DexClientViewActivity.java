
package org.decred.dex.dexandroid;

import android.app.Activity;
import android.content.Intent;
import android.os.Build;
import android.os.Bundle;
import android.util.Log;
import android.widget.ProgressBar;

import androidx.annotation.NonNull;
import androidx.annotation.RequiresApi;
//import androidx.swiperefreshlayout.widget.SwipeRefreshLayout;

import org.mozilla.geckoview.GeckoResult;
import org.mozilla.geckoview.GeckoRuntime;
import org.mozilla.geckoview.GeckoSession;
import org.mozilla.geckoview.GeckoView;
import org.mozilla.geckoview.WebRequestError;


public class DexClientViewActivity extends Activity {

    @RequiresApi(api = Build.VERSION_CODES.TIRAMISU)
    @Override
    protected void onCreate(Bundle savedInstanceState) {

        super.onCreate(savedInstanceState);
        setContentView(R.layout.activity_dexclientview);

        GeckoView geckoView = findViewById(R.id.geckoview);
        geckoView.coverUntilFirstPaint(R.color.backgroundBlue);

        ProgressBar progressBar = findViewById(R.id.progressBar);

        GeckoSession session = new GeckoSession();

        // This implements a swipe to refresh action on the web view.
        // TODO decide whether it is really needed - it conflicts with the scrolling in the Markets
        // view as it currently is.  The refresh action is useful for development really so might
        // not be useful in production.
//        SwipeRefreshLayout swipeRefreshLayout = findViewById(R.id.swipe_refresh_layout);
//        swipeRefreshLayout.setOnRefreshListener(() -> {
//            session.reload();
//            swipeRefreshLayout.setRefreshing(false);
//        });

        // Workaround for Bug 1758212
        session.setContentDelegate(new GeckoSession.ContentDelegate() {
        });

        DexCompanionApp application = (DexCompanionApp) getApplicationContext();
        GeckoViewHelper gvHelper = application.getGeckoViewHelper();
        GeckoRuntime sRuntime;
        try {
            sRuntime = gvHelper.getGeckoRuntime(this);
        } catch (Exception e) {
            errorActivity(e.toString());
            return;
        }

        session.open(sRuntime);

        session.setNavigationDelegate(new GeckoSession.NavigationDelegate() {
            @Override
            public GeckoResult<String> onLoadError(@NonNull GeckoSession session, String uri, @NonNull WebRequestError error) {
                // Error occurred while trying to load a URI
                errorActivity("Unable to connect to DEX client");
                return null;
            }
        });

        DexClient dexHost = getIntent().getSerializableExtra("dexHost", DexClient.class);
        if (dexHost == null) {
            Log.e(DexCompanionApp.LOG_TAG, "Invalid DEX host: (null)");
            finish();
            return;
        }
        geckoView.setSession(session);
        gvHelper.setProgressBar(session, progressBar);
        session.loadUri(dexHost.url());
    }

    // errorActivity navigates back to MainActivity and sends a displayable error message
    private void errorActivity(String errorStr) {
        Intent intent = new Intent(DexClientViewActivity.this, MainActivity.class);
        intent.putExtra("error", errorStr);
        startActivity(intent);
    }
}