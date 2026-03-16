package io.homoscale.android

import android.content.Context
import android.content.SharedPreferences
import org.json.JSONArray
import org.json.JSONObject
import java.util.UUID

data class SubscriptionProfile(
    val id: String,
    val name: String,
    val url: String,
)

data class AppPreferencesState(
    val subscriptions: List<SubscriptionProfile>,
    val activeSubscriptionId: String?,
    val loadedSubscriptionUrl: String,
    val enableIpv6: Boolean,
    val tunRoutingMode: String,
    val tunIncludePackages: List<String>,
    val tunExcludePackages: List<String>,
) {
    fun activeSubscription(): SubscriptionProfile? {
        return subscriptions.firstOrNull { it.id == activeSubscriptionId }
    }
}

class AppPreferencesStore(private val context: Context) {
    private val prefs: SharedPreferences =
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)

    fun load(): AppPreferencesState {
        val profiles = loadProfiles()
        val enableIpv6 = prefs.getBoolean(KEY_ENABLE_IPV6, false)
        val activeId = prefs.getString(KEY_ACTIVE_SUBSCRIPTION_ID, null)
            ?.takeIf { current -> profiles.any { it.id == current } }
            ?: profiles.firstOrNull()?.id
        val loadedSubscriptionUrl = prefs.getString(KEY_LOADED_SUBSCRIPTION_URL, null)
            .orEmpty()
            .trim()
            .ifBlank { profiles.firstOrNull { it.id == activeId }?.url.orEmpty() }
        val routingMode = loadTunRoutingMode()
        val includePackages = loadTunIncludePackages()
        val excludePackages = loadTunExcludePackages()
        return AppPreferencesState(
            subscriptions = profiles,
            activeSubscriptionId = activeId,
            loadedSubscriptionUrl = loadedSubscriptionUrl,
            enableIpv6 = enableIpv6,
            tunRoutingMode = routingMode,
            tunIncludePackages = includePackages,
            tunExcludePackages = excludePackages,
        )
    }

    fun save(state: AppPreferencesState) {
        val validActiveId = state.activeSubscriptionId
            ?.takeIf { current -> state.subscriptions.any { it.id == current } }
            ?: state.subscriptions.firstOrNull()?.id
        prefs.edit()
            .putString(KEY_SUBSCRIPTIONS_JSON, serializeProfiles(state.subscriptions).toString())
            .putString(KEY_ACTIVE_SUBSCRIPTION_ID, validActiveId)
            .putString(KEY_LOADED_SUBSCRIPTION_URL, state.loadedSubscriptionUrl.trim())
            .putBoolean(KEY_ENABLE_IPV6, state.enableIpv6)
            .putString(KEY_TUN_ROUTING_MODE, state.tunRoutingMode)
            .putString(KEY_TUN_INCLUDE_PACKAGES_JSON, serializeStringList(state.tunIncludePackages).toString())
            .putString(KEY_TUN_EXCLUDE_PACKAGES_JSON, serializeStringList(state.tunExcludePackages).toString())
            .apply()
    }

    private fun loadProfiles(): List<SubscriptionProfile> {
        val rawProfiles = prefs.getString(KEY_SUBSCRIPTIONS_JSON, null)
        if (!rawProfiles.isNullOrBlank()) {
            return deserializeProfiles(rawProfiles)
        }

        val legacyUrl = prefs.getString(KEY_LEGACY_SUBSCRIPTION_URL, "").orEmpty().trim()
        if (legacyUrl.isBlank()) {
            return emptyList()
        }
        return listOf(
            SubscriptionProfile(
                id = UUID.randomUUID().toString(),
                name = "Default subscription",
                url = legacyUrl,
            )
        )
    }

    private fun serializeProfiles(profiles: List<SubscriptionProfile>): JSONArray {
        val array = JSONArray()
        profiles.forEach { profile ->
            array.put(
                JSONObject()
                    .put("id", profile.id)
                    .put("name", profile.name)
                    .put("url", profile.url)
            )
        }
        return array
    }

    private fun loadTunExcludePackages(): List<String> {
        val rawPackages = prefs.getString(KEY_TUN_EXCLUDE_PACKAGES_JSON, null)
        if (!rawPackages.isNullOrBlank()) {
            return deserializeStringList(rawPackages)
        }
        return ConfigFiles.readTunExcludePackages(ConfigFiles.configFile(context))
    }

    private fun loadTunIncludePackages(): List<String> {
        val rawPackages = prefs.getString(KEY_TUN_INCLUDE_PACKAGES_JSON, null)
        if (!rawPackages.isNullOrBlank()) {
            return deserializeStringList(rawPackages)
        }
        return ConfigFiles.readTunIncludePackages(ConfigFiles.configFile(context))
    }

    private fun loadTunRoutingMode(): String {
        val rawMode = prefs.getString(KEY_TUN_ROUTING_MODE, null).orEmpty().trim()
        if (rawMode == ConfigFiles.TUN_ROUTE_MODE_INCLUDE || rawMode == ConfigFiles.TUN_ROUTE_MODE_EXCLUDE) {
            return rawMode
        }
        return ConfigFiles.readTunPackageMode(ConfigFiles.configFile(context))
    }

    private fun serializeStringList(values: List<String>): JSONArray {
        val array = JSONArray()
        values.forEach { value ->
            if (value.isNotBlank()) {
                array.put(value)
            }
        }
        return array
    }

    private fun deserializeProfiles(raw: String): List<SubscriptionProfile> {
        return runCatching {
            val array = JSONArray(raw)
            buildList {
                for (index in 0 until array.length()) {
                    val item = array.optJSONObject(index) ?: continue
                    val id = item.optString("id").trim()
                    val name = item.optString("name").trim()
                    val url = item.optString("url").trim()
                    if (id.isBlank() || url.isBlank()) {
                        continue
                    }
                    add(
                        SubscriptionProfile(
                            id = id,
                            name = name.ifBlank { "Subscription ${size + 1}" },
                            url = url,
                        )
                    )
                }
            }
        }.getOrDefault(emptyList())
    }

    private fun deserializeStringList(raw: String): List<String> {
        return runCatching {
            val array = JSONArray(raw)
            buildList {
                for (index in 0 until array.length()) {
                    val value = array.optString(index).trim()
                    if (value.isNotBlank()) {
                        add(value)
                    }
                }
            }
        }.getOrDefault(emptyList())
    }

    companion object {
        private const val PREFS = "homoscale"
        private const val KEY_SUBSCRIPTIONS_JSON = "subscriptions_json"
        private const val KEY_ACTIVE_SUBSCRIPTION_ID = "active_subscription_id"
        private const val KEY_LOADED_SUBSCRIPTION_URL = "loaded_subscription_url"
        private const val KEY_ENABLE_IPV6 = "enable_ipv6"
        private const val KEY_TUN_ROUTING_MODE = "tun_routing_mode"
        private const val KEY_TUN_INCLUDE_PACKAGES_JSON = "tun_include_packages_json"
        private const val KEY_TUN_EXCLUDE_PACKAGES_JSON = "tun_exclude_packages_json"
        private const val KEY_LEGACY_SUBSCRIPTION_URL = "subscription_url"
    }
}
