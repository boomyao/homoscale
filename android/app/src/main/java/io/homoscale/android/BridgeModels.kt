package io.homoscale.android

import org.json.JSONArray
import org.json.JSONObject

data class BridgeEnvelope(
    val ok: Boolean,
    val running: Boolean,
    val message: String?,
    val error: String?,
    val logPath: String?,
    val data: JSONObject?,
) {
    companion object {
        fun parse(raw: String): BridgeEnvelope {
            val payload = runCatching { JSONObject(raw) }.getOrDefault(JSONObject())
            return BridgeEnvelope(
                ok = payload.optBoolean("ok", false),
                running = payload.optBoolean("running", false),
                message = payload.optString("message").trim().ifBlank { null },
                error = payload.optString("error").trim().ifBlank { null },
                logPath = payload.optString("log_path").trim().ifBlank { null },
                data = payload.optJSONObject("data"),
            )
        }
    }
}

data class StatusOverview(
    val overall: String = "off",
    val auth: String = "off",
    val engine: String = "off",
)

data class TailnetAuthState(
    val backendState: String = "",
    val loggedIn: Boolean = false,
    val needsLogin: Boolean = false,
    val needsMachineAuth: Boolean = false,
    val authUrl: String = "",
    val tailnet: String = "",
    val magicDnsSuffix: String = "",
    val selfDnsName: String = "",
    val accessDomains: List<String> = emptyList(),
    val tailscaleIps: List<String> = emptyList(),
)

data class ProxySelectorState(
    val name: String,
    val type: String,
    val current: String,
    val options: List<String>,
)

data class EngineUiState(
    val reachable: Boolean = false,
    val error: String = "",
    val mode: String = "rule",
    val modes: List<String> = listOf("rule", "global", "direct"),
    val selectors: List<ProxySelectorState> = emptyList(),
)

data class BridgeStatusState(
    val running: Boolean = false,
    val status: StatusOverview = StatusOverview(),
    val auth: TailnetAuthState = TailnetAuthState(),
    val engine: EngineUiState = EngineUiState(),
    val configPath: String = "",
    val logPath: String = "",
    val lastError: String = "",
)

fun parseBridgeStatus(raw: String): Pair<BridgeEnvelope, BridgeStatusState> {
    val envelope = BridgeEnvelope.parse(raw)
    val data = envelope.data ?: JSONObject()
    return envelope to BridgeStatusState(
        running = envelope.running,
        status = parseStatusOverview(data.optJSONObject("status")),
        auth = parseAuthState(data.optJSONObject("auth")),
        engine = parseEngineState(data.optJSONObject("engine")),
        configPath = data.optString("configPath").trim(),
        logPath = envelope.logPath.orEmpty(),
        lastError = data.optString("lastError").trim(),
    )
}

private fun parseStatusOverview(payload: JSONObject?): StatusOverview {
    return StatusOverview(
        overall = payload?.optString("overall").orEmpty().ifBlank { "off" },
        auth = payload?.optString("auth").orEmpty().ifBlank { "off" },
        engine = payload?.optString("engine").orEmpty().ifBlank { "off" },
    )
}

internal fun parseAuthState(payload: JSONObject?): TailnetAuthState {
    return TailnetAuthState(
        backendState = payload?.optString("backend_state").orEmpty(),
        loggedIn = payload?.optBoolean("logged_in", false) == true,
        needsLogin = payload?.optBoolean("needs_login", false) == true,
        needsMachineAuth = payload?.optBoolean("needs_machine_auth", false) == true,
        authUrl = payload?.optString("auth_url").orEmpty(),
        tailnet = payload?.optString("tailnet").orEmpty(),
        magicDnsSuffix = payload?.optString("magic_dns_suffix").orEmpty(),
        selfDnsName = payload?.optString("self_dns_name").orEmpty(),
        accessDomains = payload.jsonStringList("access_domains"),
        tailscaleIps = payload.jsonStringList("tailscale_ips"),
    )
}

private fun parseEngineState(payload: JSONObject?): EngineUiState {
    val snapshot = payload?.optJSONObject("snapshot")
    val modes = snapshot.jsonStringList("mode_list").ifEmpty { listOf("rule", "global", "direct") }
    val selectors = snapshot?.optJSONArray("selectors").toObjectList { item ->
        ProxySelectorState(
            name = item.optString("name"),
            type = item.optString("type"),
            current = item.optString("now"),
            options = item.jsonStringList("options"),
        )
    }
    return EngineUiState(
        reachable = payload?.optBoolean("reachable", false) == true,
        error = payload?.optString("error").orEmpty(),
        mode = snapshot?.optString("mode").orEmpty().ifBlank { "rule" },
        modes = modes,
        selectors = selectors,
    )
}

private fun JSONObject?.jsonStringList(key: String): List<String> {
    return this?.optJSONArray(key).toStringList()
}

private fun JSONArray?.toStringList(): List<String> {
    if (this == null) {
        return emptyList()
    }
    return buildList {
        for (index in 0 until length()) {
            val value = optString(index).trim()
            if (value.isNotBlank()) {
                add(value)
            }
        }
    }
}

private inline fun <T> JSONArray?.toObjectList(transform: (JSONObject) -> T): List<T> {
    if (this == null) {
        return emptyList()
    }
    return buildList {
        for (index in 0 until length()) {
            val value = optJSONObject(index) ?: continue
            add(transform(value))
        }
    }
}
