package io.homoscale.android

import android.content.Context
import android.content.pm.ApplicationInfo
import android.content.pm.PackageManager
import android.os.Build

data class InstalledAppInfo(
    val packageName: String,
    val label: String,
    val selected: Boolean,
    val isSystemApp: Boolean,
)

object InstalledAppsCatalog {
    private val suggestedProxyPackages = linkedSetOf(
        "com.twitter.android",
        "com.google.android.youtube",
        "com.google.android.googlequicksearchbox",
        "com.google.android.apps.bard",
        "com.google.android.apps.gemini",
        "com.openai.chatgpt",
        "com.anthropic.claude",
        "ai.x.grok",
    )

    fun load(context: Context, selectedPackages: Set<String>): List<InstalledAppInfo> {
        val packageManager = context.packageManager
        val apps = loadApplications(packageManager)
        val byPackage = linkedMapOf<String, InstalledAppInfo>()

        apps.forEach { appInfo ->
            val packageName = appInfo.packageName?.trim().orEmpty()
            if (packageName.isBlank() || packageName == context.packageName) {
                return@forEach
            }
            val selected = selectedPackages.contains(packageName)
            val launchable = packageManager.getLaunchIntentForPackage(packageName) != null
            if (!launchable && !selected) {
                return@forEach
            }
            val label = runCatching { packageManager.getApplicationLabel(appInfo).toString().trim() }
                .getOrDefault("")
                .ifBlank { packageName }
            val isSystemApp =
                (appInfo.flags and ApplicationInfo.FLAG_SYSTEM) != 0 ||
                    (appInfo.flags and ApplicationInfo.FLAG_UPDATED_SYSTEM_APP) != 0
            byPackage[packageName] = InstalledAppInfo(
                packageName = packageName,
                label = label,
                selected = selected,
                isSystemApp = isSystemApp,
            )
        }

        selectedPackages.forEach { packageName ->
            if (byPackage.containsKey(packageName)) {
                return@forEach
            }
            byPackage[packageName] = InstalledAppInfo(
                packageName = packageName,
                label = packageName,
                selected = true,
                isSystemApp = false,
            )
        }

        return byPackage.values.sortedWith(
            compareByDescending<InstalledAppInfo> { it.selected }
                .thenBy { it.label.lowercase() }
                .thenBy { it.packageName.lowercase() }
        )
    }

    fun suggestedProxyApps(installedApps: List<InstalledAppInfo>): List<InstalledAppInfo> {
        return installedApps.filter { suggestedProxyPackages.contains(it.packageName) }
            .sortedBy { it.label.lowercase() }
    }

    @Suppress("DEPRECATION")
    private fun loadApplications(packageManager: PackageManager): List<ApplicationInfo> {
        return if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            packageManager.getInstalledApplications(PackageManager.ApplicationInfoFlags.of(0))
        } else {
            packageManager.getInstalledApplications(0)
        }
    }
}
