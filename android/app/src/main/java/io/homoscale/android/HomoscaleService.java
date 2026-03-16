package io.homoscale.android;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;
import android.content.IntentFilter;
import android.content.pm.PackageInfo;
import android.content.pm.PackageManager;
import android.net.ConnectivityManager;
import android.net.InetAddresses;
import android.net.LinkProperties;
import android.net.Network;
import android.net.NetworkCapabilities;
import android.net.VpnService;
import android.os.Build;
import android.os.IBinder;
import android.os.ParcelFileDescriptor;

import org.json.JSONObject;
import org.json.JSONArray;

import java.io.File;
import java.io.IOException;
import java.net.InetSocketAddress;
import java.net.InterfaceAddress;
import java.net.NetworkInterface;
import java.util.ArrayList;
import java.util.LinkedHashSet;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Enumeration;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

public final class HomoscaleService extends VpnService {
    public static final String ACTION_START = "io.homoscale.android.action.START";
    public static final String ACTION_STOP = "io.homoscale.android.action.STOP";
    public static final String EXTRA_CONFIG_PATH = "config_path";
    public static final String EXTRA_ENABLE_IPV6 = "enable_ipv6";

    private static final String CHANNEL_ID = "homoscale-runtime";
    private static final int NOTIFICATION_ID = 1001;
    private static final String VPN_DNS4 = "198.19.0.2";
    private static final String VPN_DNS6 = "fdfe:dcba:9877::2";

    private final ExecutorService executor = Executors.newSingleThreadExecutor();
    private ConnectivityManager connectivityManager;
    private ConnectivityManager.NetworkCallback defaultNetworkCallback;
    private BroadcastReceiver packageChangedReceiver;
    private volatile String activeConfigPath = "";
    private volatile boolean stopRequested = false;
    private volatile boolean running = false;

    public static Intent startIntent(Context context, String configPath, boolean enableIpv6) {
        Intent intent = new Intent(context, HomoscaleService.class);
        intent.setAction(ACTION_START);
        intent.putExtra(EXTRA_CONFIG_PATH, configPath);
        intent.putExtra(EXTRA_ENABLE_IPV6, enableIpv6);
        return intent;
    }

    public static Intent stopIntent(Context context, String configPath) {
        Intent intent = new Intent(context, HomoscaleService.class);
        intent.setAction(ACTION_STOP);
        intent.putExtra(EXTRA_CONFIG_PATH, configPath);
        return intent;
    }

    @Override
    public void onCreate() {
        super.onCreate();
        HomoscaleBridge.installVpnService(this);
        connectivityManager = getSystemService(ConnectivityManager.class);
        registerDefaultNetworkCallback();
        updateAndroidNetworkSnapshot();
        registerPackageChangedReceiver();
        refreshInstalledAppsSnapshot();
        ensureNotificationChannel();
    }

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        if (intent == null) {
            return START_NOT_STICKY;
        }

        String action = intent.getAction();
        String configPath = intent.getStringExtra(EXTRA_CONFIG_PATH);
        boolean enableIpv6 = intent.getBooleanExtra(EXTRA_ENABLE_IPV6, false);
        if (configPath == null) {
            configPath = "";
        }

        if (ACTION_STOP.equals(action)) {
            requestStop(configPath);
            return START_NOT_STICKY;
        }

        if (!ACTION_START.equals(action)) {
            return START_NOT_STICKY;
        }
        if (running) {
            updateNotification(getString(R.string.notification_running));
            return START_STICKY;
        }

        activeConfigPath = configPath;
        stopRequested = false;
        startForeground(NOTIFICATION_ID, buildNotification(getString(R.string.notification_idle)));
        final boolean ipv6Enabled = enableIpv6;
        executor.execute(() -> {
            String response = startVpnRuntime(activeConfigPath, ipv6Enabled);
            String summary = extractSummary(response, getString(R.string.notification_running));
            updateNotification(summary);
            running = response.contains("\"ok\":true");
            if (!running) {
                stopRequested = true;
                stopSelf();
            }
        });
        return START_STICKY;
    }

    @Override
    public void onDestroy() {
        if (!stopRequested && !activeConfigPath.isEmpty()) {
            requestStop(activeConfigPath);
        }
        unregisterDefaultNetworkCallback();
        unregisterPackageChangedReceiver();
        HomoscaleBridge.setInterfaceSnapshot("[]");
        HomoscaleBridge.setDefaultRouteInterface("");
        HomoscaleBridge.setInstalledAppsSnapshot("[]");
        HomoscaleBridge.clearVpnService();
        executor.shutdown();
        super.onDestroy();
    }

    @Override
    public void onRevoke() {
        requestStop(activeConfigPath);
        super.onRevoke();
    }

    @Override
    public IBinder onBind(Intent intent) {
        return null;
    }

    private void requestStop(String configPath) {
        stopRequested = true;
        running = false;
        final String path = (configPath == null || configPath.isEmpty()) ? activeConfigPath : configPath;
        executor.execute(() -> {
            String response = HomoscaleBridge.stop(path);
            updateNotification(extractSummary(response, getString(R.string.notification_stopped)));
            stopForeground(STOP_FOREGROUND_REMOVE);
            stopSelf();
        });
    }

    private void ensureNotificationChannel() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) {
            return;
        }
        NotificationManager notificationManager = getSystemService(NotificationManager.class);
        if (notificationManager == null) {
            return;
        }
        NotificationChannel channel = new NotificationChannel(
                CHANNEL_ID,
                getString(R.string.notification_channel_name),
                NotificationManager.IMPORTANCE_LOW
        );
        notificationManager.createNotificationChannel(channel);
    }

    private Notification buildNotification(String text) {
        Notification.Builder builder = Build.VERSION.SDK_INT >= Build.VERSION_CODES.O
                ? new Notification.Builder(this, CHANNEL_ID)
                : new Notification.Builder(this);
        return builder
                .setSmallIcon(android.R.drawable.stat_notify_sync)
                .setContentTitle(getString(R.string.notification_title))
                .setContentText(text)
                .setOngoing(true)
                .build();
    }

    private void updateNotification(String text) {
        NotificationManager manager = getSystemService(NotificationManager.class);
        if (manager == null) {
            return;
        }
        manager.notify(NOTIFICATION_ID, buildNotification(text));
    }

    private String startVpnRuntime(String configPath, boolean enableIpv6) {
        ParcelFileDescriptor tunInterface = null;
        try {
            updateAndroidNetworkSnapshot();
            Builder builder = new Builder()
                    .setSession(getString(R.string.notification_title))
                    .setMtu(1500)
                    .addAddress("198.19.0.1", 30)
                    .addDnsServer(VPN_DNS4)
                    .addRoute("0.0.0.0", 0);
            if (enableIpv6) {
                builder
                        .addAddress("fdfe:dcba:9877::1", 126)
                        .addDnsServer(VPN_DNS6)
                        .addRoute("::", 0);
            }
            String packageMode = readTunPackageMode(configPath);
            if (ConfigFiles.TUN_ROUTE_MODE_INCLUDE.equals(packageMode)) {
                List<String> allowedPackages = readTunAllowedPackages(configPath);
                if (allowedPackages.isEmpty()) {
                    return "{\"ok\":false,\"error\":\"whitelist mode requires at least one selected app\"}";
                }
                for (String packageName : allowedPackages) {
                    if (packageName.equals(getPackageName())) {
                        continue;
                    }
                    try {
                        builder.addAllowedApplication(packageName);
                    } catch (PackageManager.NameNotFoundException ignored) {
                    }
                }
            } else {
                try {
                    builder.addDisallowedApplication(getPackageName());
                } catch (PackageManager.NameNotFoundException error) {
                    return "{\"ok\":false,\"error\":\"exclude package failed: " + escapeJson(error.getMessage()) + "\"}";
                }
                for (String packageName : readTunDisallowedPackages(configPath)) {
                    if (packageName.equals(getPackageName())) {
                        continue;
                    }
                    try {
                        builder.addDisallowedApplication(packageName);
                    } catch (PackageManager.NameNotFoundException ignored) {
                    }
                }
            }
            tunInterface = builder.establish();
            if (tunInterface == null) {
                return "{\"ok\":false,\"error\":\"VpnService establish returned null\"}";
            }
            int tunFd = tunInterface.detachFd();
            tunInterface = null;
            return HomoscaleBridge.start(configPath, tunFd);
        } catch (Exception error) {
            return "{\"ok\":false,\"error\":\"vpn start failed: " + escapeJson(error.getMessage()) + "\"}";
        } finally {
            if (tunInterface != null) {
                try {
                    tunInterface.close();
                } catch (IOException ignored) {
                }
            }
        }
    }

    private List<String> readTunDisallowedPackages(String configPath) {
        LinkedHashSet<String> packages = new LinkedHashSet<>();
        if (configPath == null || configPath.trim().isEmpty()) {
            return new ArrayList<>();
        }
        packages.addAll(ConfigFiles.readTunExcludePackages(new File(configPath)));
        return new ArrayList<>(packages);
    }

    private List<String> readTunAllowedPackages(String configPath) {
        LinkedHashSet<String> packages = new LinkedHashSet<>();
        if (configPath == null || configPath.trim().isEmpty()) {
            return new ArrayList<>();
        }
        packages.addAll(ConfigFiles.readTunIncludePackages(new File(configPath)));
        return new ArrayList<>(packages);
    }

    private String readTunPackageMode(String configPath) {
        if (configPath == null || configPath.trim().isEmpty()) {
            return ConfigFiles.TUN_ROUTE_MODE_EXCLUDE;
        }
        return ConfigFiles.readTunPackageMode(new File(configPath));
    }

    private static String extractSummary(String json, String fallback) {
        try {
            JSONObject object = new JSONObject(json);
            if (object.optString("error").length() > 0) {
                return object.optString("error");
            }
            if (object.optString("message").length() > 0) {
                return object.optString("message");
            }
        } catch (Exception ignored) {
        }
        return fallback;
    }

    private static String escapeJson(String value) {
        if (value == null) {
            return "";
        }
        return value.replace("\\", "\\\\").replace("\"", "\\\"");
    }

    private void registerDefaultNetworkCallback() {
        if (connectivityManager == null || defaultNetworkCallback != null) {
            return;
        }
        defaultNetworkCallback = new ConnectivityManager.NetworkCallback() {
            @Override
            public void onAvailable(Network network) {
                updateAndroidNetworkSnapshot();
            }

            @Override
            public void onCapabilitiesChanged(Network network, NetworkCapabilities networkCapabilities) {
                updateAndroidNetworkSnapshot();
            }

            @Override
            public void onLost(Network network) {
                updateAndroidNetworkSnapshot();
            }
        };
        try {
            connectivityManager.registerDefaultNetworkCallback(defaultNetworkCallback);
        } catch (Exception ignored) {
            defaultNetworkCallback = null;
        }
    }

    private void unregisterDefaultNetworkCallback() {
        if (connectivityManager == null || defaultNetworkCallback == null) {
            return;
        }
        try {
            connectivityManager.unregisterNetworkCallback(defaultNetworkCallback);
        } catch (Exception ignored) {
        } finally {
            defaultNetworkCallback = null;
        }
    }

    private void registerPackageChangedReceiver() {
        if (packageChangedReceiver != null) {
            return;
        }
        packageChangedReceiver = new BroadcastReceiver() {
            @Override
            public void onReceive(Context context, Intent intent) {
                refreshInstalledAppsSnapshot();
            }
        };
        IntentFilter filter = new IntentFilter();
        filter.addAction(Intent.ACTION_PACKAGE_ADDED);
        filter.addAction(Intent.ACTION_PACKAGE_REMOVED);
        filter.addAction(Intent.ACTION_PACKAGE_CHANGED);
        filter.addDataScheme("package");
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            registerReceiver(packageChangedReceiver, filter, Context.RECEIVER_NOT_EXPORTED);
        } else {
            registerReceiver(packageChangedReceiver, filter);
        }
    }

    private void unregisterPackageChangedReceiver() {
        if (packageChangedReceiver == null) {
            return;
        }
        try {
            unregisterReceiver(packageChangedReceiver);
        } catch (Exception ignored) {
        } finally {
            packageChangedReceiver = null;
        }
    }

    private void refreshInstalledAppsSnapshot() {
        JSONArray snapshot = new JSONArray();
        try {
            PackageManager packageManager = getPackageManager();
            Map<String, List<PackageInfo>> grouped = new LinkedHashMap<>();
            for (PackageInfo info : packageManager.getInstalledPackages(0)) {
                if (info == null || info.applicationInfo == null) {
                    continue;
                }
                String uniqueName = uniqueUidName(info);
                List<PackageInfo> group = grouped.get(uniqueName);
                if (group == null) {
                    group = new ArrayList<>();
                    grouped.put(uniqueName, group);
                }
                group.add(info);
            }
            for (List<PackageInfo> group : grouped.values()) {
                if (group.isEmpty()) {
                    continue;
                }
                PackageInfo first = group.get(0);
                if (first.applicationInfo == null) {
                    continue;
                }
                JSONObject item = new JSONObject();
                item.put("uid", first.applicationInfo.uid);
                item.put("package_name", group.size() == 1 ? first.packageName : uniqueUidName(first));
                snapshot.put(item);
            }
        } catch (Exception ignored) {
            snapshot = new JSONArray();
        }
        HomoscaleBridge.setInstalledAppsSnapshot(snapshot.toString());
    }

    private static String uniqueUidName(PackageInfo info) {
        if (info.sharedUserId != null && !info.sharedUserId.trim().isEmpty()) {
            return info.sharedUserId;
        }
        return info.packageName;
    }

    private void updateAndroidNetworkSnapshot() {
        JSONArray snapshot = new JSONArray();
        try {
            Enumeration<NetworkInterface> interfaces = NetworkInterface.getNetworkInterfaces();
            while (interfaces != null && interfaces.hasMoreElements()) {
                NetworkInterface networkInterface = interfaces.nextElement();
                JSONObject item = new JSONObject();
                item.put("name", networkInterface.getName());
                item.put("index", networkInterface.getIndex());
                item.put("mtu", networkInterface.getMTU());
                item.put("flags", encodeFlags(networkInterface));

                byte[] hardwareAddress = networkInterface.getHardwareAddress();
                if (hardwareAddress != null && hardwareAddress.length > 0) {
                    item.put("hardware_addr", hexBytes(hardwareAddress));
                }

                JSONArray addrs = new JSONArray();
                for (InterfaceAddress interfaceAddress : networkInterface.getInterfaceAddresses()) {
                    if (interfaceAddress == null || interfaceAddress.getAddress() == null) {
                        continue;
                    }
                    short prefixLength = interfaceAddress.getNetworkPrefixLength();
                    if (prefixLength < 0) {
                        continue;
                    }
                    addrs.put(interfaceAddress.getAddress().getHostAddress() + "/" + prefixLength);
                }
                item.put("addrs", addrs);
                snapshot.put(item);
            }
        } catch (Exception ignored) {
            snapshot = new JSONArray();
        }
        HomoscaleBridge.setInterfaceSnapshot(snapshot.toString());

        String interfaceName = "";
        if (connectivityManager != null) {
            try {
                Network network = connectivityManager.getActiveNetwork();
                if (network != null) {
                    LinkProperties linkProperties = connectivityManager.getLinkProperties(network);
                    if (linkProperties != null && linkProperties.getInterfaceName() != null) {
                        interfaceName = linkProperties.getInterfaceName();
                    }
                }
            } catch (Exception ignored) {
                interfaceName = "";
            }
        }
        HomoscaleBridge.setDefaultRouteInterface(interfaceName);
    }

    public int queryConnectionOwnerUid(int protocol, String sourceHost, int sourcePort, String targetHost, int targetPort) {
        if (connectivityManager == null || Build.VERSION.SDK_INT < Build.VERSION_CODES.Q) {
            return -1;
        }
        try {
            InetSocketAddress source = new InetSocketAddress(
                    InetAddresses.parseNumericAddress(sourceHost),
                    sourcePort
            );
            InetSocketAddress target = new InetSocketAddress(
                    InetAddresses.parseNumericAddress(targetHost),
                    targetPort
            );
            return connectivityManager.getConnectionOwnerUid(protocol, source, target);
        } catch (Exception ignored) {
            return -1;
        }
    }

    private static int encodeFlags(NetworkInterface networkInterface) {
        int flags = 0;
        try {
            if (networkInterface.isUp()) {
                flags |= 1;
            }
            if (networkInterface.isLoopback()) {
                flags |= 1 << 2;
            }
            if (networkInterface.isPointToPoint()) {
                flags |= 1 << 3;
            }
            if (networkInterface.supportsMulticast()) {
                flags |= 1 << 4;
            }
        } catch (Exception ignored) {
        }
        return flags;
    }

    private static String hexBytes(byte[] value) {
        StringBuilder builder = new StringBuilder(value.length * 2);
        for (byte current : value) {
            builder.append(String.format("%02x", current));
        }
        return builder.toString();
    }
}
