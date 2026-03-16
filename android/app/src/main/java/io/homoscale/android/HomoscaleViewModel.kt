package io.homoscale.android

import android.app.Application
import android.content.Intent
import android.os.Build
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

data class AppRoutingUiState(
    val mode: String = ConfigFiles.TUN_ROUTE_MODE_EXCLUDE,
    val includePackages: List<String> = emptyList(),
    val excludePackages: List<String> = emptyList(),
    val installedApps: List<InstalledAppInfo> = emptyList(),
)

data class HomoscaleUiState(
    val loading: Boolean = true,
    val refreshing: Boolean = false,
    val busy: Boolean = false,
    val subscriptions: List<SubscriptionProfile> = emptyList(),
    val activeSubscriptionId: String? = null,
    val enableIpv6: Boolean = false,
    val bundledMihomoVersion: String = "",
    val runtimeDir: String = "",
    val configPath: String = "",
    val serviceRunning: Boolean = false,
    val status: StatusOverview = StatusOverview(),
    val auth: TailnetAuthState = TailnetAuthState(),
    val engine: EngineUiState = EngineUiState(),
    val appRouting: AppRoutingUiState = AppRoutingUiState(),
    val logPath: String = "",
    val lastError: String = "",
    val message: String = "",
) {
    val activeSubscription: SubscriptionProfile?
        get() = subscriptions.firstOrNull { it.id == activeSubscriptionId }
}

sealed interface UiEvent {
    data class Message(val value: String) : UiEvent
    data class OpenUrl(val value: String) : UiEvent
}

class HomoscaleViewModel(application: Application) : AndroidViewModel(application) {
    private val appContext = application.applicationContext
    private val prefsStore = AppPreferencesStore(appContext)
    private val _uiState = MutableStateFlow(initialUiState())
    private val _events = MutableSharedFlow<UiEvent>()
    private var pollJob: Job? = null

    val uiState: StateFlow<HomoscaleUiState> = _uiState.asStateFlow()
    val events: SharedFlow<UiEvent> = _events.asSharedFlow()

    init {
        viewModelScope.launch {
            refreshInstalledApps()
            syncConfig()
            refreshStatus(announceErrors = false)
            startPolling()
        }
    }

    fun connectService() {
        val current = _uiState.value
        if (current.appRouting.mode == ConfigFiles.TUN_ROUTE_MODE_INCLUDE &&
            current.appRouting.includePackages.isEmpty()
        ) {
            viewModelScope.launch {
                emitMessage("Whitelist mode needs at least one app selected before connecting.")
            }
            return
        }
        launchBusyAction("Connecting…") {
            val configPath = syncConfig()
            val intent = HomoscaleService.startIntent(appContext, configPath, _uiState.value.enableIpv6)
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                appContext.startForegroundService(intent)
            } else {
                appContext.startService(intent)
            }
            delay(1200)
            refreshStatus(announceErrors = true)
        }
    }

    fun disconnectService() {
        launchBusyAction("Disconnecting…") {
            val configPath = currentConfigPath()
            appContext.startService(HomoscaleService.stopIntent(appContext, configPath))
            delay(500)
            refreshStatus(announceErrors = true)
        }
    }

    fun loginToTailnet() {
        launchBusyAction("Starting Tailscale login…") {
            val envelope = BridgeEnvelope.parse(HomoscaleBridge.login(syncConfig()))
            val auth = parseAuthState(envelope.data?.optJSONObject("auth"))
            refreshStatus(announceErrors = false)
            when {
                !envelope.error.isNullOrBlank() -> emitMessage(envelope.error)
                auth.authUrl.isNotBlank() -> _events.emit(UiEvent.OpenUrl(auth.authUrl))
                _uiState.value.auth.authUrl.isNotBlank() -> _events.emit(UiEvent.OpenUrl(_uiState.value.auth.authUrl))
                else -> emitMessage(envelope.message ?: "Tailscale login requested")
            }
        }
    }

    fun openPendingAuthUrl() {
        viewModelScope.launch {
            val url = _uiState.value.auth.authUrl
            if (url.isNotBlank()) {
                _events.emit(UiEvent.OpenUrl(url))
            } else {
                emitMessage("No active Tailscale auth URL.")
            }
        }
    }

    fun logoutTailnet() {
        launchBusyAction("Logging out…") {
            val envelope = BridgeEnvelope.parse(HomoscaleBridge.logout(currentConfigPath()))
            refreshStatus(announceErrors = false)
            if (!envelope.error.isNullOrBlank()) {
                emitMessage(envelope.error)
            } else {
                emitMessage(envelope.message ?: "Logged out from Tailscale")
            }
        }
    }

    fun setIpv6Enabled(enabled: Boolean) {
        val current = _uiState.value
        prefsStore.save(
            AppPreferencesState(
                subscriptions = current.subscriptions,
                activeSubscriptionId = current.activeSubscriptionId,
                enableIpv6 = enabled,
                tunRoutingMode = current.appRouting.mode,
                tunIncludePackages = current.appRouting.includePackages,
                tunExcludePackages = current.appRouting.excludePackages,
            )
        )
        _uiState.update { it.copy(enableIpv6 = enabled) }
        viewModelScope.launch {
            syncConfig()
            emitMessage("IPv6 ${if (enabled) "enabled" else "disabled"}. Reconnect to apply.")
        }
    }

    fun setActiveSubscription(profileId: String) {
        savePreferences(activeSubscriptionId = profileId)
        _uiState.update { it.copy(activeSubscriptionId = profileId) }
        viewModelScope.launch {
            syncConfig()
            emitMessage("Active subscription updated.")
        }
    }

    fun saveSubscription(editingId: String?, name: String, url: String) {
        val trimmedName = name.trim()
        val trimmedUrl = url.trim()
        if (trimmedUrl.isBlank()) {
            viewModelScope.launch { emitMessage("Subscription URL is required.") }
            return
        }

        val current = _uiState.value
        val updated = current.subscriptions.toMutableList()
        val profile = SubscriptionProfile(
            id = editingId ?: java.util.UUID.randomUUID().toString(),
            name = trimmedName.ifBlank { "Subscription ${updated.size + 1}" },
            url = trimmedUrl,
        )

        val existingIndex = updated.indexOfFirst { it.id == profile.id }
        if (existingIndex >= 0) {
            updated[existingIndex] = profile
        } else {
            updated.add(profile)
        }

        val activeId = current.activeSubscriptionId ?: profile.id
        savePreferences(
            subscriptions = updated,
            activeSubscriptionId = activeId,
        )
        _uiState.update { it.copy(subscriptions = updated, activeSubscriptionId = activeId) }
        viewModelScope.launch {
            syncConfig()
            emitMessage(if (existingIndex >= 0) "Subscription updated." else "Subscription added.")
        }
    }

    fun deleteSubscription(profileId: String) {
        val current = _uiState.value
        val updated = current.subscriptions.filterNot { it.id == profileId }
        val activeId = current.activeSubscriptionId?.takeIf { id -> updated.any { it.id == id } }
            ?: updated.firstOrNull()?.id
        savePreferences(
            subscriptions = updated,
            activeSubscriptionId = activeId,
        )
        _uiState.update { it.copy(subscriptions = updated, activeSubscriptionId = activeId) }
        viewModelScope.launch {
            syncConfig()
            emitMessage("Subscription removed.")
        }
    }

    fun setPackageBypass(packageName: String, enabled: Boolean) {
        val trimmedPackage = packageName.trim()
        if (trimmedPackage.isBlank()) {
            return
        }

        val current = _uiState.value
        val includePackages = if (current.appRouting.mode == ConfigFiles.TUN_ROUTE_MODE_INCLUDE) {
            if (enabled) {
                (current.appRouting.includePackages + trimmedPackage).distinct().sorted()
            } else {
                current.appRouting.includePackages.filterNot { it == trimmedPackage }
            }
        } else {
            current.appRouting.includePackages
        }
        val excludePackages = if (current.appRouting.mode == ConfigFiles.TUN_ROUTE_MODE_EXCLUDE) {
            if (enabled) {
                (current.appRouting.excludePackages + trimmedPackage).distinct().sorted()
            } else {
                current.appRouting.excludePackages.filterNot { it == trimmedPackage }
            }
        } else {
            current.appRouting.excludePackages
        }

        savePreferences(
            tunIncludePackages = includePackages,
            tunExcludePackages = excludePackages,
        )
        _uiState.update {
            it.copy(
                appRouting = it.appRouting.copy(
                    includePackages = includePackages,
                    excludePackages = excludePackages,
                ),
            )
        }
        viewModelScope.launch {
            refreshInstalledApps()
            syncConfig()
            emitMessage(
                when {
                    current.appRouting.mode == ConfigFiles.TUN_ROUTE_MODE_INCLUDE && enabled ->
                        "Added to proxy app list. Reconnect to apply."
                    current.appRouting.mode == ConfigFiles.TUN_ROUTE_MODE_INCLUDE ->
                        "Removed from proxy app list. Reconnect to apply."
                    enabled ->
                        "Added to VPN bypass list. Reconnect to apply."
                    else ->
                        "Removed from VPN bypass list. Reconnect to apply."
                }
            )
        }
    }

    fun setRoutingMode(mode: String) {
        if (mode != ConfigFiles.TUN_ROUTE_MODE_INCLUDE && mode != ConfigFiles.TUN_ROUTE_MODE_EXCLUDE) {
            return
        }
        savePreferences(tunRoutingMode = mode)
        _uiState.update {
            it.copy(
                appRouting = it.appRouting.copy(mode = mode),
            )
        }
        viewModelScope.launch {
            refreshInstalledApps()
            syncConfig()
            emitMessage(
                if (mode == ConfigFiles.TUN_ROUTE_MODE_INCLUDE) {
                    "Switched to whitelist mode. Reconnect to apply."
                } else {
                    "Switched to blacklist mode. Reconnect to apply."
                }
            )
        }
    }

    fun addSuggestedProxyApps() {
        val current = _uiState.value
        val suggestedPackages = InstalledAppsCatalog.suggestedProxyApps(current.appRouting.installedApps)
            .map { it.packageName }
        if (suggestedPackages.isEmpty()) {
            viewModelScope.launch { emitMessage("No installed default proxy apps were found.") }
            return
        }
        val updatedIncludePackages = (current.appRouting.includePackages + suggestedPackages).distinct().sorted()
        savePreferences(
            tunRoutingMode = ConfigFiles.TUN_ROUTE_MODE_INCLUDE,
            tunIncludePackages = updatedIncludePackages,
        )
        _uiState.update {
            it.copy(
                appRouting = it.appRouting.copy(
                    mode = ConfigFiles.TUN_ROUTE_MODE_INCLUDE,
                    includePackages = updatedIncludePackages,
                ),
            )
        }
        viewModelScope.launch {
            refreshInstalledApps()
            syncConfig()
            emitMessage("Added installed default proxy apps. Reconnect to apply.")
        }
    }

    fun setProxyMode(mode: String) {
        launchBusyAction("Switching proxy mode…") {
            val envelope = BridgeEnvelope.parse(HomoscaleBridge.setProxyMode(currentConfigPath(), mode))
            refreshStatus(announceErrors = false)
            if (!envelope.error.isNullOrBlank()) {
                emitMessage(envelope.error)
            }
        }
    }

    fun selectProxy(groupName: String, proxyName: String) {
        launchBusyAction("Switching node…") {
            val envelope = BridgeEnvelope.parse(
                HomoscaleBridge.selectProxyGroup(currentConfigPath(), groupName, proxyName)
            )
            refreshStatus(announceErrors = false)
            if (!envelope.error.isNullOrBlank()) {
                emitMessage(envelope.error)
            }
        }
    }

    fun manualRefresh() {
        viewModelScope.launch {
            refreshInstalledApps()
            refreshStatus(announceErrors = true)
        }
    }

    private fun startPolling() {
        pollJob?.cancel()
        pollJob = viewModelScope.launch {
            while (isActive) {
                refreshStatus(announceErrors = false)
                delay(3000)
            }
        }
    }

    private suspend fun refreshStatus(announceErrors: Boolean) {
        _uiState.update { it.copy(refreshing = true) }
        val raw = withContext(Dispatchers.IO) {
            HomoscaleBridge.status(currentConfigPath())
        }
        val (envelope, snapshot) = parseBridgeStatus(raw)
        _uiState.update {
            it.copy(
                loading = false,
                refreshing = false,
                serviceRunning = snapshot.running,
                status = snapshot.status,
                auth = snapshot.auth,
                engine = snapshot.engine,
                configPath = snapshot.configPath.ifBlank { it.configPath },
                logPath = snapshot.logPath,
                lastError = listOfNotNull(envelope.error, snapshot.lastError).firstOrNull().orEmpty(),
                message = envelope.message.orEmpty(),
            )
        }
        if (announceErrors && !envelope.error.isNullOrBlank()) {
            emitMessage(envelope.error)
        }
    }

    private suspend fun syncConfig(): String {
        val activeUrl = _uiState.value.activeSubscription?.url.orEmpty()
        val configPath = withContext(Dispatchers.IO) {
            ConfigFiles.writeDefaultConfig(
                appContext,
                activeUrl,
                _uiState.value.enableIpv6,
                _uiState.value.appRouting.mode,
                _uiState.value.appRouting.includePackages,
                _uiState.value.appRouting.excludePackages,
            )
        }
        _uiState.update { it.copy(configPath = configPath) }
        return configPath
    }

    private suspend fun refreshInstalledApps() {
        val selectedPackages = activePackageSelection().toSet()
        val installedApps = withContext(Dispatchers.IO) {
            InstalledAppsCatalog.load(appContext, selectedPackages)
        }
        _uiState.update {
            it.copy(
                appRouting = it.appRouting.copy(installedApps = installedApps),
            )
        }
    }

    private fun currentConfigPath(): String {
        return _uiState.value.configPath.ifBlank { ConfigFiles.configFile(appContext).absolutePath }
    }

    private fun initialUiState(): HomoscaleUiState {
        val prefs = prefsStore.load()
        return HomoscaleUiState(
            subscriptions = prefs.subscriptions,
            activeSubscriptionId = prefs.activeSubscriptionId,
            enableIpv6 = prefs.enableIpv6,
            appRouting = AppRoutingUiState(
                mode = prefs.tunRoutingMode,
                includePackages = prefs.tunIncludePackages,
                excludePackages = prefs.tunExcludePackages,
            ),
            bundledMihomoVersion = BundledMihomo.version(appContext),
            runtimeDir = ConfigFiles.runtimeDir(appContext).absolutePath,
            configPath = ConfigFiles.configFile(appContext).absolutePath,
        )
    }

    private fun savePreferences(
        subscriptions: List<SubscriptionProfile> = _uiState.value.subscriptions,
        activeSubscriptionId: String? = _uiState.value.activeSubscriptionId,
        enableIpv6: Boolean = _uiState.value.enableIpv6,
        tunRoutingMode: String = _uiState.value.appRouting.mode,
        tunIncludePackages: List<String> = _uiState.value.appRouting.includePackages,
        tunExcludePackages: List<String> = _uiState.value.appRouting.excludePackages,
    ) {
        prefsStore.save(
            AppPreferencesState(
                subscriptions = subscriptions,
                activeSubscriptionId = activeSubscriptionId,
                enableIpv6 = enableIpv6,
                tunRoutingMode = tunRoutingMode,
                tunIncludePackages = tunIncludePackages,
                tunExcludePackages = tunExcludePackages,
            )
        )
    }

    private fun activePackageSelection(): List<String> {
        return if (_uiState.value.appRouting.mode == ConfigFiles.TUN_ROUTE_MODE_INCLUDE) {
            _uiState.value.appRouting.includePackages
        } else {
            _uiState.value.appRouting.excludePackages
        }
    }

    private fun launchBusyAction(message: String, block: suspend () -> Unit) {
        viewModelScope.launch {
            _uiState.update { it.copy(busy = true, message = message) }
            runCatching { block() }
                .onFailure { emitMessage(it.message ?: "Unexpected error") }
            _uiState.update { it.copy(busy = false) }
        }
    }

    private suspend fun emitMessage(message: String) {
        _events.emit(UiEvent.Message(message))
    }

    override fun onCleared() {
        pollJob?.cancel()
        super.onCleared()
    }
}
