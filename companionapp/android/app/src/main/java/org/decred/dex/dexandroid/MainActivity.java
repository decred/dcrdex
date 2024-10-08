package org.decred.dex.dexandroid;

import android.content.BroadcastReceiver;
import android.content.ComponentName;
import android.content.Context;
import android.content.Intent;
import android.content.IntentFilter;
import android.content.ServiceConnection;
import android.graphics.drawable.Drawable;
import android.os.Build;
import android.os.Bundle;
import android.os.IBinder;
import android.util.Log;
import android.view.MenuItem;
import android.widget.TextView;
import android.widget.Toast;

import androidx.activity.result.ActivityResultLauncher;
import androidx.annotation.RequiresApi;
import androidx.appcompat.app.AppCompatActivity;
import androidx.core.content.ContextCompat;
import androidx.recyclerview.widget.LinearLayoutManager;
import androidx.recyclerview.widget.RecyclerView;

import com.google.android.material.floatingactionbutton.FloatingActionButton;

import org.torproject.jni.TorService;

public class MainActivity extends AppCompatActivity {

    private RecyclerView recyclerView;

    @RequiresApi(api = Build.VERSION_CODES.TIRAMISU)
    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        setContentView(R.layout.activity_main);

        // floating action button for pairing a new DEX client
        FloatingActionButton fab = findViewById(R.id.fab);

        // DEX client chooser list view
        recyclerView = findViewById(R.id.recycler_view);
        recyclerView.setHasFixedSize(true);

        RecyclerView.LayoutManager layoutManager = new LinearLayoutManager(this);
        recyclerView.setLayoutManager(layoutManager);

        PreferenceManager preferenceManager = new PreferenceManager(this);
        DexClientChooserAdapter mAdapter = new DexClientChooserAdapter(this, preferenceManager, false);
        recyclerView.setAdapter(mAdapter);

        // tor connection status indicator
        TextView connectionStatusText = findViewById(R.id.connection_status_text);
        Drawable offlineDrawable = ContextCompat.getDrawable(this, R.drawable.ic_offline);
        connectionStatusText.setCompoundDrawablesWithIntrinsicBounds(offlineDrawable, null, null, null);

        // If we were sent back to the main activity due to an error loading a page, display error message
        String errorStr = getIntent().getStringExtra("error");
        if (errorStr != null) {
            Log.e(DexCompanionApp.LOG_TAG, "LoadError: " + errorStr);
            Toast.makeText(MainActivity.this, errorStr, Toast.LENGTH_SHORT).show();
        }

        ActivityResultLauncher<Void> launcher = registerForActivityResult(new QRCodeScannerContract(), newClientURL -> {
            if (!newClientURL.isEmpty()) {

                DexClient newItem;

                try {
                    newItem = mAdapter.addItem(newClientURL);
                } catch (Exception e) {
                    Toast.makeText(MainActivity.this, "DEX client already exists", Toast.LENGTH_SHORT).show();
                    return;
                }
                Toast.makeText(MainActivity.this, "Paired DEX: " + newItem.name(), Toast.LENGTH_LONG).show();
            }
        });

        fab.setOnClickListener(view -> launcher.launch(null));

        // onReceive fires twice.  Tor is ready when status is ON
        BroadcastReceiver broadcastReceiver = new BroadcastReceiver() {
            @Override
            public void onReceive(Context context, Intent intent) {
                String status = intent.getStringExtra(TorService.EXTRA_STATUS);

                if (status == null || !status.equals(TorService.STATUS_ON)) {
                    mAdapter.setConnected(false);
                    Drawable offlineDrawable = ContextCompat.getDrawable(context, R.drawable.ic_offline);
                    connectionStatusText.setCompoundDrawablesWithIntrinsicBounds(offlineDrawable, null, null, null);
                    connectionStatusText.setText("Tor offline");
                    return;
                }

                mAdapter.setConnected(true);
                Drawable onlineDrawable = ContextCompat.getDrawable(context, R.drawable.ic_online);
                connectionStatusText.setCompoundDrawablesWithIntrinsicBounds(onlineDrawable, null, null, null);
                connectionStatusText.setText("Tor online");
            }
        };

        registerReceiver(broadcastReceiver, new IntentFilter(TorService.ACTION_STATUS), Context.RECEIVER_NOT_EXPORTED);

        // Create tor service connection
        ServiceConnection conn = new ServiceConnection() {
            @Override
            public void onServiceConnected(ComponentName name, IBinder service) {
                TorService torService = ((TorService.LocalBinder) service).getService();
                while (torService.getTorControlConnection() == null) {
                    try {
                        Thread.sleep(500);
                    } catch (InterruptedException e) {
                        Log.e(DexCompanionApp.LOG_TAG, "InterruptedException: " + e);
                        Toast.makeText(MainActivity.this, "Tor control connection failed", Toast.LENGTH_SHORT).show();
                    }
                }
                Log.i(DexCompanionApp.LOG_TAG, "Got Tor control connection: " + name.toString());
            }

            @Override
            public void onServiceDisconnected(ComponentName name) {
                Log.i(DexCompanionApp.LOG_TAG, "onServiceDisconnected: " + name.toString());
                Toast.makeText(MainActivity.this, "Tor service disconnected", Toast.LENGTH_SHORT).show();
            }
        };

        bindService(new Intent(this, TorService.class), conn, Context.BIND_AUTO_CREATE);
    }

    @Override
    public boolean onContextItemSelected(MenuItem item) {
        if (item.getItemId() == R.id.delete) {
            DexClientChooserAdapter adapter = ((DexClientChooserAdapter) recyclerView.getAdapter());
            if (adapter == null) {
                return false;
            }
            RecyclerView.ViewHolder viewHolder = adapter.getViewHolder();
            int position = viewHolder.getAbsoluteAdapterPosition();
            if (position < 0) {
                // FIXME this shouldn't occur but it can be -1 when deleting the last item in the list.
                return false;
            }
            ((DexClientChooserAdapter) recyclerView.getAdapter()).removeItem(position);
            return true;
        }
        return super.onContextItemSelected(item);
    }
}