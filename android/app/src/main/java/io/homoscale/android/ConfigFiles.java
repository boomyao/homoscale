package io.homoscale.android;

import android.content.Context;

import java.io.BufferedReader;
import java.io.File;
import java.io.FileInputStream;
import java.io.FileOutputStream;
import java.io.IOException;
import java.io.InputStreamReader;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;

public final class ConfigFiles {
    public static final String TUN_ROUTE_MODE_INCLUDE = "include";
    public static final String TUN_ROUTE_MODE_EXCLUDE = "exclude";

    private ConfigFiles() {
    }

    public static File runtimeDir(Context context) {
        return new File(context.getFilesDir(), "homoscale-runtime");
    }

    public static File configFile(Context context) {
        return new File(runtimeDir(context), "homoscale.yaml");
    }

    public static List<String> readTunExcludePackages(File configFile) {
        return readTunStringList(configFile, "exclude_package");
    }

    public static List<String> readTunIncludePackages(File configFile) {
        return readTunStringList(configFile, "include_package");
    }

    public static String readTunPackageMode(File configFile) {
        if (hasTunListKey(configFile, "include_package")) {
            return TUN_ROUTE_MODE_INCLUDE;
        }
        return TUN_ROUTE_MODE_EXCLUDE;
    }

    public static String writeDefaultConfig(Context context, String subscriptionUrl, boolean enableIpv6) throws IOException {
        File configFile = configFile(context);
        return writeDefaultConfig(
                context,
                subscriptionUrl,
                enableIpv6,
                readTunPackageMode(configFile),
                readTunIncludePackages(configFile),
                readTunExcludePackages(configFile)
        );
    }

    public static String writeDefaultConfig(
            Context context,
            String subscriptionUrl,
            boolean enableIpv6,
            String packageMode,
            List<String> includePackages,
            List<String> excludePackages
    ) throws IOException {
        File runtimeDir = runtimeDir(context);
        File tailscaleDir = new File(runtimeDir, "tailscale");
        File engineDir = new File(runtimeDir, "engine");

        if (!runtimeDir.mkdirs() && !runtimeDir.isDirectory()) {
            throw new IOException("Could not create runtime dir: " + runtimeDir);
        }
        if (!tailscaleDir.mkdirs() && !tailscaleDir.isDirectory()) {
            throw new IOException("Could not create tailscale dir: " + tailscaleDir);
        }
        if (!engineDir.mkdirs() && !engineDir.isDirectory()) {
            throw new IOException("Could not create engine dir: " + engineDir);
        }
        File configFile = configFile(context);
        StringBuilder builder = new StringBuilder();
        builder.append("runtime_dir: ").append(yamlQuote(runtimeDir.getAbsolutePath())).append('\n');
        builder.append("tailscale:\n");
        builder.append("  backend: embedded\n");
        builder.append("  state_dir: ").append(yamlQuote(tailscaleDir.getAbsolutePath())).append('\n');
        builder.append("engine:\n");
        builder.append("  binary: \"embedded-mihomo\"\n");
        builder.append("  config_path: ").append(yamlQuote(new File(engineDir, "config.yaml").getAbsolutePath())).append('\n');
        builder.append("  working_dir: ").append(yamlQuote(engineDir.getAbsolutePath())).append('\n');
        builder.append("  state_file: ").append(yamlQuote(new File(engineDir, "engine.pid.json").getAbsolutePath())).append('\n');
        builder.append("  controller_addr: \"127.0.0.1:9090\"\n");
        builder.append("  mixed_port: 7890\n");
        builder.append("  secret: \"\"\n");
        builder.append("  ipv6: ").append(enableIpv6 ? "true" : "false").append('\n');
        if (!subscriptionUrl.trim().isEmpty()) {
            builder.append("  subscription_url: ").append(yamlQuote(subscriptionUrl.trim())).append('\n');
        }
        builder.append("  tun:\n");
        builder.append("    enable: true\n");
        builder.append("    mtu: 1500\n");
        builder.append("    stack: \"gvisor\"\n");
        builder.append("    auto_route: false\n");
        builder.append("    auto_detect_interface: false\n");
        builder.append("    strict_route: false\n");
        builder.append("    file_descriptor: 3\n");
        builder.append("    dns_hijack:\n");
        builder.append("      - \"any:53\"\n");
        builder.append("      - \"tcp://any:53\"\n");
        builder.append("    inet4_address:\n");
        builder.append("      - \"198.19.0.1/30\"\n");
        if (enableIpv6) {
            builder.append("    inet6_address:\n");
            builder.append("      - \"fdfe:dcba:9877::1/126\"\n");
        }
        boolean includeMode = TUN_ROUTE_MODE_INCLUDE.equals(packageMode);
        if (includeMode) {
            builder.append("    include_package:\n");
            for (String packageName : includePackages) {
                builder.append("      - ").append(yamlQuote(packageName)).append('\n');
            }
        } else if (!excludePackages.isEmpty()) {
            builder.append("    exclude_package:\n");
            for (String packageName : excludePackages) {
                builder.append("      - ").append(yamlQuote(packageName)).append('\n');
            }
        }
        try (FileOutputStream outputStream = new FileOutputStream(configFile, false)) {
            outputStream.write(builder.toString().getBytes(StandardCharsets.UTF_8));
        }
        return configFile.getAbsolutePath();
    }

    private static List<String> readTunStringList(File configFile, String key) {
        List<String> values = new ArrayList<>();
        if (configFile == null || !configFile.isFile()) {
            return values;
        }

        String listPrefix = "    " + key + ":";
        boolean insideTun = false;
        boolean collecting = false;
        try (BufferedReader reader = new BufferedReader(
                new InputStreamReader(new FileInputStream(configFile), StandardCharsets.UTF_8)
        )) {
            String line;
            while ((line = reader.readLine()) != null) {
                String trimmed = line.trim();
                if (!insideTun) {
                    if ("tun:".equals(trimmed) || "tun: |".equals(trimmed)) {
                        insideTun = line.startsWith("  ");
                    }
                    continue;
                }

                if (line.startsWith("  ") && !line.startsWith("    ")) {
                    break;
                }

                if (line.startsWith(listPrefix)) {
                    collecting = true;
                    continue;
                }

                if (!collecting) {
                    continue;
                }

                if (line.startsWith("      - ")) {
                    String value = line.substring("      - ".length()).trim();
                    if ((value.startsWith("'") && value.endsWith("'")) || (value.startsWith("\"") && value.endsWith("\""))) {
                        value = value.substring(1, value.length() - 1);
                    }
                    value = value.replace("''", "'");
                    if (!value.isEmpty()) {
                        values.add(value);
                    }
                    continue;
                }

                if (line.startsWith("    ")) {
                    break;
                }
            }
        } catch (IOException ignored) {
            return new ArrayList<>();
        }
        return values;
    }

    private static boolean hasTunListKey(File configFile, String key) {
        if (configFile == null || !configFile.isFile()) {
            return false;
        }

        String listPrefix = "    " + key + ":";
        boolean insideTun = false;
        try (BufferedReader reader = new BufferedReader(
                new InputStreamReader(new FileInputStream(configFile), StandardCharsets.UTF_8)
        )) {
            String line;
            while ((line = reader.readLine()) != null) {
                String trimmed = line.trim();
                if (!insideTun) {
                    if ("tun:".equals(trimmed) || "tun: |".equals(trimmed)) {
                        insideTun = line.startsWith("  ");
                    }
                    continue;
                }

                if (line.startsWith("  ") && !line.startsWith("    ")) {
                    break;
                }

                if (line.startsWith(listPrefix)) {
                    return true;
                }
            }
        } catch (IOException ignored) {
            return false;
        }
        return false;
    }

    private static String yamlQuote(String value) {
        return "'" + value.replace("'", "''") + "'";
    }
}
