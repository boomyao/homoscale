package io.homoscale.android;

public final class HomoscaleBridge {
    static {
        System.loadLibrary("homoscale");
        System.loadLibrary("homoscale_jni");
    }

    private HomoscaleBridge() {
    }

    public static native String version();

    public static native String status(String configPath);

    public static native String start(String configPath, int tunFd);

    public static native String stop(String configPath);

    public static native String login(String configPath);

    public static native String logout(String configPath);

    public static native String setProxyMode(String configPath, String mode);

    public static native String selectProxyGroup(String configPath, String groupName, String proxyName);

    public static native void installVpnService(HomoscaleService service);

    public static native void clearVpnService();

    public static native void setDefaultRouteInterface(String interfaceName);

    public static native void setInterfaceSnapshot(String snapshotJson);

    public static native void setInstalledAppsSnapshot(String snapshotJson);
}
