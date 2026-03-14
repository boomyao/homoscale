package io.homoscale.android;

import android.content.Context;

import java.io.File;
import java.io.FileOutputStream;
import java.io.IOException;
import java.nio.charset.StandardCharsets;

public final class ConfigFiles {
    private ConfigFiles() {
    }

    public static File runtimeDir(Context context) {
        return new File(context.getFilesDir(), "homoscale-runtime");
    }

    public static File configFile(Context context) {
        return new File(runtimeDir(context), "homoscale.yaml");
    }

    public static String writeDefaultConfig(Context context, String subscriptionUrl, boolean enableIpv6) throws IOException {
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
        try (FileOutputStream outputStream = new FileOutputStream(configFile, false)) {
            outputStream.write(builder.toString().getBytes(StandardCharsets.UTF_8));
        }
        return configFile.getAbsolutePath();
    }

    private static String yamlQuote(String value) {
        return "'" + value.replace("'", "''") + "'";
    }
}
