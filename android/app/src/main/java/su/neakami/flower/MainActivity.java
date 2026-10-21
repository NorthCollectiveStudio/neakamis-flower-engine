package su.neakami.flower;

import android.annotation.SuppressLint;
import android.app.Activity;
import android.content.Context;
import android.graphics.Color;
import android.media.AudioAttributes;
import android.os.Build;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.os.VibrationEffect;
import android.os.Vibrator;
import android.os.VibratorManager;
import android.view.View;
import android.view.WindowManager;
import android.webkit.JavascriptInterface;
import android.webkit.WebSettings;
import android.webkit.WebView;
import android.webkit.WebViewClient;

import org.json.JSONArray;

import java.io.File;
import java.io.FileOutputStream;
import java.io.InputStream;

import su.neakami.flowerengine.mobile.Engine;

public class MainActivity extends Activity {

    private static final String GAME_PKG = "game.nkpkg";
    private static final String PROD_PKG = "prodgame.nkpkgenc";
    private static final String RUNTIME_PKG = "runtime.nkpkg";
    private static final String RUNTIME_PKG_ENC = "runtime.nkpkgenc";
    private static final int ENGINE_PORT = 18080;

    private Engine engine;
    private WebView web;
    private Handler handler;
    private boolean quitting;
    private String bootUrl = "";
    private final Object engineLock = new Object();

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);

        handler = new Handler(Looper.getMainLooper());
        engine = new Engine();

        setContentView(R.layout.booting);
        applyImmersive();

        new Thread(new Runnable() {
            @Override
            public void run() {
                final File dataDir = new File(getFilesDir(), "flwdata");
                final File pkg = ensurePackage(dataDir);
                final File runtimePkg = ensureRuntime(dataDir);

                String pkgPath = pkg != null ? pkg.getAbsolutePath() : "";
                String runtimePath = runtimePkg != null ? runtimePkg.getAbsolutePath() : "";

                final String url;
                try {
                    synchronized (engineLock) {
                        engine.start(dataDir.getAbsolutePath(), pkgPath, runtimePath, ENGINE_PORT);
                        url = engine.bootURL();
                    }
                } catch (final Exception e) {
                    handler.post(new Runnable() {
                        @Override
                        public void run() {
                            throw new RuntimeException("flower engine start failed: " + e.getMessage(), e);
                        }
                    });
                    return;
                }
                handler.post(new Runnable() {
                    @Override
                    public void run() {
                        if (quitting) {
                            return;
                        }
                        setupWebView(url);
                    }
                });
            }
        }, "flw-boot").start();
    }

    @SuppressLint("SetJavaScriptEnabled")
    private void setupWebView(String url) {
        bootUrl = url;
        if (BuildConfig.DEBUG) {
            WebView.setWebContentsDebuggingEnabled(true);
        }
        web = new WebView(this);
        web.setBackgroundColor(Color.BLACK);
        WebSettings s = web.getSettings();
        s.setJavaScriptEnabled(true);
        s.setDomStorageEnabled(true);
        s.setMediaPlaybackRequiresUserGesture(false);
        s.setMixedContentMode(WebSettings.MIXED_CONTENT_ALWAYS_ALLOW);
        s.setSupportZoom(false);
        s.setBuiltInZoomControls(false);
        s.setDisplayZoomControls(false);
        s.setCacheMode(WebSettings.LOAD_NO_CACHE);
        web.setOverScrollMode(View.OVER_SCROLL_NEVER);
        web.setVerticalScrollBarEnabled(false);
        web.setHorizontalScrollBarEnabled(false);
        web.addJavascriptInterface(new NativeBridge(), "flwNativeAndroid");
        web.setWebViewClient(new WebViewClient() {
            @Override
            public boolean shouldOverrideUrlLoading(WebView view, String url) {
                return !url.startsWith("http://127.0.0.1");
            }
        });
        setContentView(web);
        applyImmersive();
        web.loadUrl(bootUrl);
        pollEvents();
    }

    private File ensurePackage(File dataDir) {
        // The assets of the apk win: an update of the app must not run the
        // game of the previous installation.
        if (assetExists(PROD_PKG)) {
            removeOther(dataDir, GAME_PKG);
            return ensureAsset(dataDir, PROD_PKG);
        }
        if (assetExists(GAME_PKG)) {
            removeOther(dataDir, PROD_PKG);
            return ensureAsset(dataDir, GAME_PKG);
        }
        File prod = new File(dataDir, PROD_PKG);
        if (prod.isFile()) return prod;
        File dev = new File(dataDir, GAME_PKG);
        if (dev.isFile()) return dev;
        return null;
    }

    // Removes the package of the other kind: it is 180 MB of dead weight.
    private void removeOther(File dataDir, String name) {
        try {
            File f = new File(dataDir, name);
            if (f.isFile()) f.delete();
            File s = new File(dataDir, name + ".stamp");
            if (s.isFile()) s.delete();
        } catch (Exception e) {
            // not important
        }
    }

    private File ensureRuntime(File dataDir) {
        if (assetExists(RUNTIME_PKG_ENC)) {
            return ensureAsset(dataDir, RUNTIME_PKG_ENC);
        }
        if (assetExists(RUNTIME_PKG)) {
            return ensureAsset(dataDir, RUNTIME_PKG);
        }
        File enc = new File(dataDir, RUNTIME_PKG_ENC);
        if (enc.isFile()) return enc;
        File plain = new File(dataDir, RUNTIME_PKG);
        if (plain.isFile()) return plain;
        return null;
    }


    private boolean assetExists(String name) {
        try {
            InputStream in = getAssets().open(name);
            in.close();
            return true;
        } catch (Exception e) {
            return false;
        }
    }

    // The stamp of an extracted asset. The packages are extracted again
    // when the apk was updated: without that the app would keep running the
    // game and the runtime of the previous installation.
    private String assetStamp(String name) {
        try {
            long len = 0;
            InputStream in = getAssets().open(name);
            try {
                len = in.available();
            } finally {
                in.close();
            }
            long updated = getPackageManager().getPackageInfo(getPackageName(), 0).lastUpdateTime;
            return updated + ":" + len;
        } catch (Exception e) {
            return "";
        }
    }

    private File ensureAsset(File dataDir, String name) {
        File out = new File(dataDir, name);
        File stamp = new File(dataDir, name + ".stamp");
        String want = assetStamp(name);
        String have = "";
        try {
            java.io.FileInputStream fs = new java.io.FileInputStream(stamp);
            byte[] sb = new byte[128];
            int n = fs.read(sb);
            fs.close();
            if (n > 0) have = new String(sb, 0, n);
        } catch (Exception e) {
            have = "";
        }
        if (out.isFile() && out.length() > 0 && want.equals(have)) {
            return out;
        }
        try {
            dataDir.mkdirs();
            File tmp = new File(dataDir, name + ".tmp");
            InputStream in = getAssets().open(name);
            FileOutputStream fo = new FileOutputStream(tmp);
            byte[] buf = new byte[65536];
            int n;
            while ((n = in.read(buf)) > 0) {
                fo.write(buf, 0, n);
            }
            fo.close();
            in.close();
            if (!tmp.renameTo(out)) {
                throw new RuntimeException("rename failed");
            }
            FileOutputStream fs = new FileOutputStream(stamp);
            fs.write(want.getBytes());
            fs.close();
        } catch (Exception e) {
            throw new RuntimeException("package extract failed: " + name, e);
        }
        return out;
    }

    private void applyImmersive() {
        View decor = getWindow().getDecorView();
        decor.setSystemUiVisibility(
                View.SYSTEM_UI_FLAG_IMMERSIVE_STICKY
                        | View.SYSTEM_UI_FLAG_FULLSCREEN
                        | View.SYSTEM_UI_FLAG_HIDE_NAVIGATION
                        | View.SYSTEM_UI_FLAG_LAYOUT_STABLE
                        | View.SYSTEM_UI_FLAG_LAYOUT_FULLSCREEN
                        | View.SYSTEM_UI_FLAG_LAYOUT_HIDE_NAVIGATION);
        getWindow().addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON);
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
            WindowManager.LayoutParams lp = getWindow().getAttributes();
            lp.layoutInDisplayCutoutMode =
                    WindowManager.LayoutParams.LAYOUT_IN_DISPLAY_CUTOUT_MODE_SHORT_EDGES;
            getWindow().setAttributes(lp);
        }
    }

    @Override
    public void onWindowFocusChanged(boolean hasFocus) {
        super.onWindowFocusChanged(hasFocus);
        if (hasFocus) {
            applyImmersive();
        }
    }

    private void pollEvents() {
        if (quitting) return;
        try {
            String raw;
            synchronized (engineLock) {
                raw = engine.getEvents();
            }
            if (raw != null && raw.length() > 2) {
                JSONArray arr = new JSONArray(raw);
                for (int i = 0; i < arr.length(); i++) {
                    JSONArray ev = arr.getJSONArray(i);
                    String name = ev.getString(0);
                    if ("game.exit".equals(name)) {
                        quitGame();
                        return;
                    }
                }
            }
        } catch (Exception e) {
        }
        handler.postDelayed(this::pollEvents, 400);
    }

    private void quitGame() {
        if (quitting) return;
        quitting = true;
        try {
            synchronized (engineLock) {
                engine.stop();
            }
        } catch (Exception e) {
        }
        finish();
    }

    @Override
    public void onBackPressed() {
        quitGame();
    }

    @Override
    protected void onDestroy() {
        if (!quitting) {
            try {
                synchronized (engineLock) {
                    engine.stop();
                }
            } catch (Exception e) {
            }
            quitting = true;
        }
        super.onDestroy();
    }

    class NativeBridge {
        @JavascriptInterface
        public String call(String name, String args) {
            try {
                synchronized (engineLock) {
                    return engine.handleNative(name, args);
                }
            } catch (Exception e) {
                return "{\"ok\":false,\"e\":\"" + e.getMessage() + "\"}";
            }
        }

        @JavascriptInterface
        public void vibrate(int ms) {
            try {
                final int duration = Math.max(1, Math.min(ms, 2000));
                Vibrator vib;
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
                    VibratorManager vm =
                            (VibratorManager) getSystemService(Context.VIBRATOR_MANAGER_SERVICE);
                    vib = vm != null ? vm.getDefaultVibrator() : null;
                } else {
                    vib = (Vibrator) getSystemService(Context.VIBRATOR_SERVICE);
                }
                if (vib == null || !vib.hasVibrator()) return;
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                    vib.vibrate(VibrationEffect.createOneShot(duration,
                            VibrationEffect.DEFAULT_AMPLITUDE));
                } else {
                    vib.vibrate(duration);
                }
            } catch (Exception e) {
            }
        }
    }
}
