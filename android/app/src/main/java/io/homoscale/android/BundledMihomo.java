package io.homoscale.android;

import android.content.Context;

import java.io.IOException;
import java.io.InputStream;
import java.nio.charset.StandardCharsets;

public final class BundledMihomo {
    private static final String ASSET_DIR = "mihomo";
    private static final String ASSET_VERSION = ASSET_DIR + "/version.txt";

    private BundledMihomo() {
    }

    public static String version(Context context) {
        try (InputStream inputStream = context.getAssets().open(ASSET_VERSION)) {
            byte[] bytes = inputStream.readAllBytes();
            return new String(bytes, StandardCharsets.UTF_8).trim();
        } catch (IOException error) {
            return "unknown";
        }
    }
}
