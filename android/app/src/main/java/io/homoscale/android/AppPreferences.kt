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
    val enableIpv6: Boolean,
) {
    fun activeSubscription(): SubscriptionProfile? {
        return subscriptions.firstOrNull { it.id == activeSubscriptionId }
    }
}

class AppPreferencesStore(context: Context) {
    private val prefs: SharedPreferences =
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)

    fun load(): AppPreferencesState {
        val profiles = loadProfiles()
        val enableIpv6 = prefs.getBoolean(KEY_ENABLE_IPV6, false)
        val activeId = prefs.getString(KEY_ACTIVE_SUBSCRIPTION_ID, null)
            ?.takeIf { current -> profiles.any { it.id == current } }
            ?: profiles.firstOrNull()?.id
        return AppPreferencesState(
            subscriptions = profiles,
            activeSubscriptionId = activeId,
            enableIpv6 = enableIpv6,
        )
    }

    fun save(state: AppPreferencesState) {
        val validActiveId = state.activeSubscriptionId
            ?.takeIf { current -> state.subscriptions.any { it.id == current } }
            ?: state.subscriptions.firstOrNull()?.id
        prefs.edit()
            .putString(KEY_SUBSCRIPTIONS_JSON, serializeProfiles(state.subscriptions).toString())
            .putString(KEY_ACTIVE_SUBSCRIPTION_ID, validActiveId)
            .putBoolean(KEY_ENABLE_IPV6, state.enableIpv6)
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

    companion object {
        private const val PREFS = "homoscale"
        private const val KEY_SUBSCRIPTIONS_JSON = "subscriptions_json"
        private const val KEY_ACTIVE_SUBSCRIPTION_ID = "active_subscription_id"
        private const val KEY_ENABLE_IPV6 = "enable_ipv6"
        private const val KEY_LEGACY_SUBSCRIPTION_URL = "subscription_url"
    }
}
