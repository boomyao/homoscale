package io.homoscale.android;

import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;
import android.content.SharedPreferences;
import android.net.VpnService;
import android.os.Build;
import android.util.Log;

import java.io.File;
import java.io.FileOutputStream;
import java.nio.charset.StandardCharsets;

public final class DebugCommandReceiver extends BroadcastReceiver {
    public static final String ACTION_WRITE_CONFIG = "io.homoscale.android.debug.WRITE_CONFIG";
    public static final String ACTION_START_SERVICE = "io.homoscale.android.debug.START_SERVICE";
    public static final String ACTION_STOP_SERVICE = "io.homoscale.android.debug.STOP_SERVICE";
    public static final String ACTION_STATUS = "io.homoscale.android.debug.STATUS";
    public static final String EXTRA_SUBSCRIPTION_URL = "subscription_url";
    public static final String EXTRA_CONFIG_PATH = "config_path";

    private static final String TAG = "HomoscaleDebug";
    private static final String PREFS = "homoscale";
    private static final String PREF_SUBSCRIPTION_URL = "subscription_url";
    private static final String PREF_ENABLE_IPV6 = "enable_ipv6";
    private static final String EXTRA_ENABLE_IPV6 = "enable_ipv6";
    private static final String DEBUG_LOG_NAME = "debug-command.log";

    @Override
    public void onReceive(Context context, Intent intent) {
        if (intent == null || intent.getAction() == null) {
            return;
        }

        try {
            String action = intent.getAction();
            if (ACTION_WRITE_CONFIG.equals(action)) {
                String configPath = writeConfig(context, intent);
                appendDebugLog(context, "write-config ok path=" + configPath);
                Log.i(TAG, "write-config ok path=" + configPath);
                return;
            }

            if (ACTION_START_SERVICE.equals(action)) {
                if (VpnService.prepare(context) != null) {
                    appendDebugLog(context, "start-service blocked: VPN permission has not been granted");
                    Log.w(TAG, "start-service blocked: VPN permission has not been granted");
                    return;
                }
                String configPath = intent.getStringExtra(EXTRA_CONFIG_PATH);
                if (configPath == null || configPath.trim().isEmpty()) {
                    configPath = writeConfig(context, intent);
                }
                boolean enableIpv6 = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
                        .getBoolean(PREF_ENABLE_IPV6, false);
                Intent serviceIntent = HomoscaleService.startIntent(context, configPath, enableIpv6);
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                    context.startForegroundService(serviceIntent);
                } else {
                    context.startService(serviceIntent);
                }
                appendDebugLog(context, "start-service requested config=" + configPath);
                Log.i(TAG, "start-service requested config=" + configPath);
                return;
            }

            if (ACTION_STOP_SERVICE.equals(action)) {
                String configPath = intent.getStringExtra(EXTRA_CONFIG_PATH);
                if (configPath == null || configPath.trim().isEmpty()) {
                    configPath = ConfigFiles.configFile(context).getAbsolutePath();
                }
                context.startService(HomoscaleService.stopIntent(context, configPath));
                appendDebugLog(context, "stop-service requested config=" + configPath);
                Log.i(TAG, "stop-service requested config=" + configPath);
                return;
            }

            if (ACTION_STATUS.equals(action)) {
                String configPath = intent.getStringExtra(EXTRA_CONFIG_PATH);
                if (configPath == null || configPath.trim().isEmpty()) {
                    configPath = ConfigFiles.configFile(context).getAbsolutePath();
                }
                String status = HomoscaleBridge.status(configPath);
                appendDebugLog(context, "status " + status);
                Log.i(TAG, "status " + status);
            }
        } catch (Exception error) {
            appendDebugLog(context, "debug command failed: " + error);
            Log.e(TAG, "debug command failed", error);
        }
    }

    private static String writeConfig(Context context, Intent intent) throws Exception {
        SharedPreferences preferences = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE);
        String subscriptionUrl = intent.getStringExtra(EXTRA_SUBSCRIPTION_URL);
        boolean enableIpv6 = intent.hasExtra(EXTRA_ENABLE_IPV6)
                ? intent.getBooleanExtra(EXTRA_ENABLE_IPV6, false)
                : preferences.getBoolean(PREF_ENABLE_IPV6, false);
        if (subscriptionUrl == null || subscriptionUrl.trim().isEmpty()) {
            subscriptionUrl = preferences.getString(PREF_SUBSCRIPTION_URL, "");
        } else {
            subscriptionUrl = subscriptionUrl.trim();
        }
        preferences.edit()
                .putString(PREF_SUBSCRIPTION_URL, subscriptionUrl)
                .putBoolean(PREF_ENABLE_IPV6, enableIpv6)
                .apply();
        return ConfigFiles.writeDefaultConfig(context, subscriptionUrl, enableIpv6);
    }

    private static void appendDebugLog(Context context, String message) {
        try {
            File runtimeDir = ConfigFiles.runtimeDir(context);
            if (!runtimeDir.exists() && !runtimeDir.mkdirs()) {
                return;
            }
            File logFile = new File(runtimeDir, DEBUG_LOG_NAME);
            String line = System.currentTimeMillis() + " " + message + "\n";
            try (FileOutputStream outputStream = new FileOutputStream(logFile, true)) {
                outputStream.write(line.getBytes(StandardCharsets.UTF_8));
            }
        } catch (Exception ignored) {
        }
    }
}
