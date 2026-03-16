package io.homoscale.android

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.net.VpnService
import android.os.Build
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.activity.viewModels
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.Add
import androidx.compose.material.icons.rounded.CheckCircle
import androidx.compose.material.icons.rounded.Delete
import androidx.compose.material.icons.rounded.Edit
import androidx.compose.material.icons.rounded.Language
import androidx.compose.material.icons.rounded.OpenInNew
import androidx.compose.material.icons.rounded.PowerSettingsNew
import androidx.compose.material.icons.rounded.Public
import androidx.compose.material.icons.rounded.Refresh
import androidx.compose.material.icons.rounded.Route
import androidx.compose.material.icons.rounded.Sync
import androidx.compose.material.icons.rounded.Tune
import androidx.compose.material.icons.rounded.VpnKey
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.AssistChip
import androidx.compose.material3.AssistChipDefaults
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DividerDefaults
import androidx.compose.material3.ElevatedCard
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LargeTopAppBar
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedCard
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.SegmentedButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.core.content.ContextCompat
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.homoscale.android.ui.theme.HomoscaleTheme
import kotlinx.coroutines.flow.collectLatest

class MainActivity : ComponentActivity() {
    private val viewModel by viewModels<HomoscaleViewModel>()
    private var pendingConnect = false

    private val notificationPermissionLauncher =
        registerForActivityResult(ActivityResultContracts.RequestPermission()) {
            continueConnectFlow()
        }

    private val vpnPermissionLauncher =
        registerForActivityResult(ActivityResultContracts.StartActivityForResult()) { result ->
            if (result.resultCode == RESULT_OK) {
                viewModel.connectService()
            }
            pendingConnect = false
        }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()

        setContent {
            val uiState by viewModel.uiState.collectAsStateWithLifecycle()
            val snackbarHostState = remember { SnackbarHostState() }
            val uriHandler = LocalUriHandler.current

            LaunchedEffect(Unit) {
                viewModel.events.collectLatest { event ->
                    when (event) {
                        is UiEvent.Message -> snackbarHostState.showSnackbar(event.value)
                        is UiEvent.OpenUrl -> {
                            runCatching { uriHandler.openUri(event.value) }
                                .getOrElse {
                                    startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(event.value)))
                                }
                        }
                    }
                }
            }

            HomoscaleTheme {
                HomoscaleApp(
                    uiState = uiState,
                    snackbarHostState = snackbarHostState,
                    onRefresh = viewModel::manualRefresh,
                    onConnect = ::beginConnectFlow,
                    onDisconnect = viewModel::disconnectService,
                    onLogin = viewModel::loginToTailnet,
                    onOpenAuth = viewModel::openPendingAuthUrl,
                    onLogout = viewModel::logoutTailnet,
                    onToggleIpv6 = viewModel::setIpv6Enabled,
                    onSetMode = viewModel::setProxyMode,
                    onSelectProxy = viewModel::selectProxy,
                    onSelectSubscription = viewModel::setActiveSubscription,
                    onSaveSubscription = viewModel::saveSubscription,
                    onDeleteSubscription = viewModel::deleteSubscription,
                    onSetRoutingMode = viewModel::setRoutingMode,
                    onSetPackageBypass = viewModel::setPackageBypass,
                    onAddSuggestedProxyApps = viewModel::addSuggestedProxyApps,
                )
            }
        }
    }

    private fun beginConnectFlow() {
        pendingConnect = true
        if (Build.VERSION.SDK_INT >= 33 &&
            ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
        ) {
            notificationPermissionLauncher.launch(Manifest.permission.POST_NOTIFICATIONS)
            return
        }
        continueConnectFlow()
    }

    private fun continueConnectFlow() {
        if (!pendingConnect) {
            return
        }
        val vpnIntent = VpnService.prepare(this)
        if (vpnIntent != null) {
            vpnPermissionLauncher.launch(vpnIntent)
        } else {
            viewModel.connectService()
            pendingConnect = false
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun HomoscaleApp(
    uiState: HomoscaleUiState,
    snackbarHostState: SnackbarHostState,
    onRefresh: () -> Unit,
    onConnect: () -> Unit,
    onDisconnect: () -> Unit,
    onLogin: () -> Unit,
    onOpenAuth: () -> Unit,
    onLogout: () -> Unit,
    onToggleIpv6: (Boolean) -> Unit,
    onSetMode: (String) -> Unit,
    onSelectProxy: (String, String) -> Unit,
    onSelectSubscription: (String) -> Unit,
    onSaveSubscription: (String?, String, String) -> Unit,
    onDeleteSubscription: (String) -> Unit,
    onSetRoutingMode: (String) -> Unit,
    onSetPackageBypass: (String, Boolean) -> Unit,
    onAddSuggestedProxyApps: () -> Unit,
) {
    var editingProfile by rememberSaveable(stateSaver = subscriptionEditorSaver()) { mutableStateOf<SubscriptionEditorState?>(null) }
    var selectorDialog by rememberSaveable(stateSaver = selectorDialogSaver()) { mutableStateOf<SelectorDialogState?>(null) }

    Scaffold(
        snackbarHost = { SnackbarHost(snackbarHostState) },
        topBar = {
            LargeTopAppBar(
                title = {
                    Column {
                        Text("homoscale")
                        Text(
                            text = "Android workspace router",
                            style = MaterialTheme.typography.bodyMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                },
                actions = {
                    IconButton(onClick = onRefresh, enabled = !uiState.loading && !uiState.busy) {
                        Icon(Icons.Rounded.Refresh, contentDescription = "Refresh")
                    }
                },
            )
        },
    ) { innerPadding ->
        if (uiState.loading) {
            Box(
                modifier = Modifier
                    .fillMaxSize()
                    .padding(innerPadding),
                contentAlignment = Alignment.Center,
            ) {
                CircularProgressIndicator()
            }
            return@Scaffold
        }

        LazyColumn(
            modifier = Modifier
                .fillMaxSize()
                .background(MaterialTheme.colorScheme.background),
            contentPadding = PaddingValues(
                start = 20.dp,
                end = 20.dp,
                top = innerPadding.calculateTopPadding() + 12.dp,
                bottom = innerPadding.calculateBottomPadding() + 24.dp,
            ),
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            item {
                HeroConnectionCard(
                    uiState = uiState,
                    onConnect = onConnect,
                    onDisconnect = onDisconnect,
                    onLogin = onLogin,
                    onOpenAuth = onOpenAuth,
                    onLogout = onLogout,
                )
            }

            item {
                DiagnosticsCard(uiState)
            }

            item {
                SubscriptionCard(
                    uiState = uiState,
                    onAdd = {
                        editingProfile = SubscriptionEditorState(null, "", "")
                    },
                    onEdit = { profile ->
                        editingProfile = SubscriptionEditorState(profile.id, profile.name, profile.url)
                    },
                    onSelect = onSelectSubscription,
                    onDelete = onDeleteSubscription,
                    onToggleIpv6 = onToggleIpv6,
                )
            }

            item {
                AppRoutingCard(
                    uiState = uiState,
                    onSetMode = onSetRoutingMode,
                    onTogglePackage = onSetPackageBypass,
                    onAddSuggestedProxyApps = onAddSuggestedProxyApps,
                )
            }

            item {
                ProxyModeCard(
                    uiState = uiState,
                    onSetMode = onSetMode,
                )
            }

            item {
                Text(
                    text = "Rule groups",
                    style = MaterialTheme.typography.titleLarge,
                    fontWeight = FontWeight.SemiBold,
                )
            }

            if (uiState.engine.selectors.isEmpty()) {
                item {
                    OutlinedCard {
                        Text(
                            text = if (uiState.engine.reachable) {
                                "No selectable rule groups were exposed by the current Mihomo config."
                            } else {
                                "Connect the engine first. Rule-group node selection becomes available once the controller is reachable."
                            },
                            modifier = Modifier.padding(18.dp),
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
            } else {
                items(uiState.engine.selectors, key = { it.name }) { selector ->
                    SelectorCard(
                        selector = selector,
                        enabled = !uiState.busy && uiState.engine.reachable,
                        onOpen = {
                            selectorDialog = SelectorDialogState(
                                groupName = selector.name,
                                current = selector.current,
                                options = selector.options,
                            )
                        },
                    )
                }
            }
        }
    }

    editingProfile?.let { dialog ->
        SubscriptionEditorDialog(
            initialState = dialog,
            onDismiss = { editingProfile = null },
            onConfirm = { id, name, url ->
                onSaveSubscription(id, name, url)
                editingProfile = null
            },
        )
    }

    selectorDialog?.let { dialog ->
        SelectorPickerDialog(
            state = dialog,
            onDismiss = { selectorDialog = null },
            onSelect = { option ->
                onSelectProxy(dialog.groupName, option)
                selectorDialog = null
            },
        )
    }
}

@Composable
private fun HeroConnectionCard(
    uiState: HomoscaleUiState,
    onConnect: () -> Unit,
    onDisconnect: () -> Unit,
    onLogin: () -> Unit,
    onOpenAuth: () -> Unit,
    onLogout: () -> Unit,
) {
    val gradient = Brush.linearGradient(
        listOf(
            MaterialTheme.colorScheme.primaryContainer,
            MaterialTheme.colorScheme.tertiaryContainer,
        )
    )

    ElevatedCard(
        colors = CardDefaults.elevatedCardColors(
            containerColor = MaterialTheme.colorScheme.surface,
        ),
        elevation = CardDefaults.elevatedCardElevation(defaultElevation = 2.dp),
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .background(gradient)
                .padding(20.dp),
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.Top,
            ) {
                Column(
                    modifier = Modifier.weight(1f),
                    verticalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    StatusPill(
                        text = when {
                            uiState.serviceRunning -> "Connected"
                            uiState.auth.loggedIn -> "Ready"
                            else -> "Needs login"
                        }
                    )
                    Text(
                        text = when {
                            uiState.serviceRunning -> "Traffic is being routed through your tailnet workspace."
                            uiState.auth.loggedIn -> "Tailnet session is ready. Start the service when you want device traffic managed."
                            else -> "Sign in to Tailscale, then bring the runtime online."
                        },
                        style = MaterialTheme.typography.headlineSmall,
                        fontWeight = FontWeight.Bold,
                        color = MaterialTheme.colorScheme.onPrimaryContainer,
                    )
                    Text(
                        text = uiState.auth.tailnet.ifBlank { "No tailnet authenticated yet" },
                        style = MaterialTheme.typography.bodyLarge,
                        color = MaterialTheme.colorScheme.onPrimaryContainer.copy(alpha = 0.84f),
                    )
                }

                Surface(
                    modifier = Modifier.size(48.dp),
                    shape = CircleShape,
                    color = MaterialTheme.colorScheme.onPrimaryContainer.copy(alpha = 0.08f),
                ) {
                    Box(contentAlignment = Alignment.Center) {
                        Icon(
                            imageVector = if (uiState.serviceRunning) Icons.Rounded.CheckCircle else Icons.Rounded.Route,
                            contentDescription = null,
                            tint = MaterialTheme.colorScheme.onPrimaryContainer,
                        )
                    }
                }
            }

            QuickStatsRow(uiState)

            Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                Button(
                    onClick = if (uiState.serviceRunning) onDisconnect else onConnect,
                    enabled = !uiState.busy,
                ) {
                    Icon(Icons.Rounded.PowerSettingsNew, contentDescription = null)
                    Spacer(Modifier.size(8.dp))
                    Text(if (uiState.serviceRunning) "Disconnect" else "Connect")
                }

                when {
                    uiState.auth.loggedIn -> {
                        OutlinedButton(onClick = onLogout, enabled = !uiState.busy) {
                            Icon(Icons.Rounded.VpnKey, contentDescription = null)
                            Spacer(Modifier.size(8.dp))
                            Text("Logout")
                        }
                    }

                    uiState.auth.authUrl.isNotBlank() -> {
                        OutlinedButton(onClick = onOpenAuth, enabled = !uiState.busy) {
                            Icon(Icons.Rounded.OpenInNew, contentDescription = null)
                            Spacer(Modifier.size(8.dp))
                            Text("Open login")
                        }
                    }

                    else -> {
                        OutlinedButton(onClick = onLogin, enabled = !uiState.busy) {
                            Icon(Icons.Rounded.VpnKey, contentDescription = null)
                            Spacer(Modifier.size(8.dp))
                            Text("Login")
                        }
                    }
                }
            }

            if (uiState.auth.accessDomains.isNotEmpty()) {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Text(
                        text = "Reachable tailnet names",
                        style = MaterialTheme.typography.labelLarge,
                        color = MaterialTheme.colorScheme.onPrimaryContainer.copy(alpha = 0.72f),
                    )
                    DomainChipRow(uiState.auth.accessDomains)
                }
            }
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun QuickStatsRow(uiState: HomoscaleUiState) {
    FlowRow(
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        InfoChip("Subscription", uiState.activeSubscription?.name ?: "Tailnet only")
        InfoChip("Mode", uiState.engine.mode.replaceFirstChar { it.uppercase() })
        InfoChip("IPv6", if (uiState.enableIpv6) "On" else "Off")
        if (uiState.auth.selfDnsName.isNotBlank()) {
            InfoChip("Device", uiState.auth.selfDnsName)
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun DomainChipRow(domains: List<String>) {
    FlowRow(
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        domains.forEach { domain ->
            AssistChip(
                onClick = {},
                label = { Text(domain, maxLines = 1, overflow = TextOverflow.Ellipsis) },
                leadingIcon = {
                    Icon(Icons.Rounded.Language, contentDescription = null, modifier = Modifier.size(18.dp))
                },
                colors = AssistChipDefaults.assistChipColors(
                    containerColor = MaterialTheme.colorScheme.surface.copy(alpha = 0.72f),
                ),
            )
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun AppRoutingCard(
    uiState: HomoscaleUiState,
    onSetMode: (String) -> Unit,
    onTogglePackage: (String, Boolean) -> Unit,
    onAddSuggestedProxyApps: () -> Unit,
) {
    var query by rememberSaveable { mutableStateOf("") }
    val normalizedQuery = query.trim()
    val includeMode = uiState.appRouting.mode == ConfigFiles.TUN_ROUTE_MODE_INCLUDE
    val selectedPackages = if (includeMode) {
        uiState.appRouting.includePackages.toSet()
    } else {
        uiState.appRouting.excludePackages.toSet()
    }
    val selectedApps = uiState.appRouting.installedApps.filter { it.packageName in selectedPackages }
    val suggestedApps = InstalledAppsCatalog.suggestedProxyApps(uiState.appRouting.installedApps)
        .filterNot { selectedPackages.contains(it.packageName) }
    val visibleApps = uiState.appRouting.installedApps.filter { app ->
        normalizedQuery.isBlank() ||
            app.label.contains(normalizedQuery, ignoreCase = true) ||
            app.packageName.contains(normalizedQuery, ignoreCase = true)
    }

    Card {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(18.dp),
            verticalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Column(modifier = Modifier.weight(1f)) {
                    Text("App routing", style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold)
                    Text(
                        if (includeMode) {
                            "Only selected apps enter Homoscale VPN. Everything else stays on the system network."
                        } else {
                            "Selected apps skip the Android VPN entirely. Use this for media and local-service apps that should stay on the system network."
                        },
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                Surface(
                    shape = CircleShape,
                    color = MaterialTheme.colorScheme.secondaryContainer,
                ) {
                    Icon(
                        imageVector = Icons.Rounded.Public,
                        contentDescription = null,
                        modifier = Modifier.padding(10.dp),
                        tint = MaterialTheme.colorScheme.onSecondaryContainer,
                    )
                }
            }

            SingleChoiceSegmentedButtonRow(modifier = Modifier.fillMaxWidth()) {
                SegmentedButton(
                    selected = includeMode,
                    onClick = { onSetMode(ConfigFiles.TUN_ROUTE_MODE_INCLUDE) },
                    shape = SegmentedButtonDefaults.itemShape(index = 0, count = 2),
                ) {
                    Text("Only selected apps")
                }
                SegmentedButton(
                    selected = !includeMode,
                    onClick = { onSetMode(ConfigFiles.TUN_ROUTE_MODE_EXCLUDE) },
                    shape = SegmentedButtonDefaults.itemShape(index = 1, count = 2),
                ) {
                    Text("All except selected")
                }
            }

            if (includeMode && suggestedApps.isNotEmpty()) {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        horizontalArrangement = Arrangement.SpaceBetween,
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Column(modifier = Modifier.weight(1f)) {
                            Text("Suggested defaults", style = MaterialTheme.typography.labelLarge)
                            Text(
                                "Installed apps that usually need proxy access.",
                                style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                        FilledActionChip("Add defaults", Icons.Rounded.Add, onAddSuggestedProxyApps)
                    }
                    FlowRow(
                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                        verticalArrangement = Arrangement.spacedBy(8.dp),
                    ) {
                        suggestedApps.forEach { app ->
                            AssistChip(
                                onClick = { onTogglePackage(app.packageName, true) },
                                label = {
                                    Text(
                                        app.label,
                                        maxLines = 1,
                                        overflow = TextOverflow.Ellipsis,
                                    )
                                },
                            )
                        }
                    }
                }
            }

            if (selectedApps.isEmpty()) {
                OutlinedCard {
                    Text(
                        text = if (includeMode) {
                            "No proxy app selected yet. Pick apps below to send only those apps through Homoscale."
                        } else {
                            "No app bypass configured yet. Pick apps below to exclude them from Homoscale VPN routing."
                        },
                        modifier = Modifier.padding(16.dp),
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            } else {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Text(
                        if (includeMode) "Using Homoscale now" else "Bypassing now",
                        style = MaterialTheme.typography.labelLarge,
                    )
                    FlowRow(
                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                        verticalArrangement = Arrangement.spacedBy(8.dp),
                    ) {
                        selectedApps.forEach { app ->
                            FilterChip(
                                selected = true,
                                onClick = { onTogglePackage(app.packageName, false) },
                                label = {
                                    Text(
                                        app.label,
                                        maxLines = 1,
                                        overflow = TextOverflow.Ellipsis,
                                    )
                                },
                                leadingIcon = {
                                    Icon(Icons.Rounded.CheckCircle, contentDescription = null, modifier = Modifier.size(18.dp))
                                },
                            )
                        }
                    }
                }
            }

            OutlinedTextField(
                modifier = Modifier.fillMaxWidth(),
                value = query,
                onValueChange = { query = it },
                label = { Text("Search apps") },
                placeholder = {
                    Text(
                        if (includeMode) {
                            "YouTube, ChatGPT, Claude…"
                        } else {
                            "bilibili, weibo, meituan…"
                        }
                    )
                },
                singleLine = true,
            )

            if (uiState.appRouting.installedApps.isEmpty()) {
                OutlinedCard {
                    Text(
                        text = "No launchable apps were discovered yet.",
                        modifier = Modifier.padding(16.dp),
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            } else {
                Text(
                    text = "${visibleApps.size} apps",
                    style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                LazyColumn(
                    modifier = Modifier
                        .fillMaxWidth()
                        .heightIn(max = 420.dp),
                    verticalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    items(visibleApps, key = { it.packageName }) { app ->
                        AppRoutingItem(
                            app = app,
                            selected = app.packageName in selectedPackages,
                            enabled = !uiState.busy,
                            modeLabel = if (includeMode) "Use Homoscale" else "Bypass",
                            onToggle = { checked -> onTogglePackage(app.packageName, checked) },
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun DiagnosticsCard(uiState: HomoscaleUiState) {
    Card {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(18.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            Text("Runtime", style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold)
            MetadataLine("Bundled Mihomo", uiState.bundledMihomoVersion)
            MetadataLine("Status", "${uiState.status.overall}/${uiState.status.auth}/${uiState.status.engine}")
            MetadataLine("Backend", uiState.auth.backendState.ifBlank { "Unknown" })
            MetadataLine("Runtime dir", uiState.runtimeDir)
            if (uiState.configPath.isNotBlank()) {
                MetadataLine("Config", uiState.configPath)
            }
            if (uiState.logPath.isNotBlank()) {
                MetadataLine("Log", uiState.logPath)
            }
            if (uiState.lastError.isNotBlank()) {
                HorizontalDivider(color = DividerDefaults.color.copy(alpha = 0.7f))
                Text(
                    text = uiState.lastError,
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.error,
                )
            }
        }
    }
}

@Composable
private fun SubscriptionCard(
    uiState: HomoscaleUiState,
    onAdd: () -> Unit,
    onEdit: (SubscriptionProfile) -> Unit,
    onSelect: (String) -> Unit,
    onDelete: (String) -> Unit,
    onToggleIpv6: (Boolean) -> Unit,
) {
    Card {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(18.dp),
            verticalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Column {
                    Text("Subscriptions", style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold)
                    Text(
                        "Manage provider URLs and decide which profile drives the embedded Mihomo config.",
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                FilledActionChip("Add", Icons.Rounded.Add, onAdd)
            }

            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Column(modifier = Modifier.weight(1f)) {
                    Text("IPv6", style = MaterialTheme.typography.labelLarge)
                    Text(
                        "Keep this off unless your current nodes prove stable for IPv6 egress.",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                Switch(
                    checked = uiState.enableIpv6,
                    onCheckedChange = onToggleIpv6,
                )
            }

            if (uiState.subscriptions.isEmpty()) {
                OutlinedCard {
                    Text(
                        text = "No subscription profile yet. You can still route tailnet traffic, but public proxy groups and rule selectors need a provider URL.",
                        modifier = Modifier.padding(16.dp),
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            } else {
                uiState.subscriptions.forEach { profile ->
                    SubscriptionItem(
                        profile = profile,
                        active = profile.id == uiState.activeSubscriptionId,
                        onSelect = { onSelect(profile.id) },
                        onEdit = { onEdit(profile) },
                        onDelete = { onDelete(profile.id) },
                    )
                }
            }
        }
    }
}

@Composable
private fun AppRoutingItem(
    app: InstalledAppInfo,
    selected: Boolean,
    enabled: Boolean,
    modeLabel: String,
    onToggle: (Boolean) -> Unit,
) {
    val containerColor = if (selected) {
        MaterialTheme.colorScheme.secondaryContainer.copy(alpha = 0.52f)
    } else {
        MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.2f)
    }

    Surface(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(20.dp))
            .clickable(enabled = enabled) { onToggle(!selected) },
        color = containerColor,
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 16.dp, vertical = 14.dp),
            horizontalArrangement = Arrangement.SpaceBetween,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(
                modifier = Modifier.weight(1f),
                verticalArrangement = Arrangement.spacedBy(4.dp),
            ) {
                Text(app.label, style = MaterialTheme.typography.titleSmall, fontWeight = FontWeight.SemiBold)
                Text(
                    app.packageName,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                if (app.isSystemApp) {
                    Text(
                        text = "System app",
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                Text(
                    text = modeLabel,
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Switch(
                checked = selected,
                onCheckedChange = onToggle,
                enabled = enabled,
            )
        }
    }
}

@Composable
private fun ProxyModeCard(
    uiState: HomoscaleUiState,
    onSetMode: (String) -> Unit,
) {
    Card {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(18.dp),
            verticalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Column(modifier = Modifier.weight(1f)) {
                    Text("Traffic mode", style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold)
                    Text(
                        text = if (uiState.engine.reachable) {
                            "Switch Mihomo between rule, global and direct without leaving the app."
                        } else {
                            "Connect the engine to enable live mode switching."
                        },
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                Surface(
                    shape = CircleShape,
                    color = MaterialTheme.colorScheme.secondaryContainer,
                ) {
                    Icon(
                        imageVector = Icons.Rounded.Tune,
                        contentDescription = null,
                        modifier = Modifier.padding(10.dp),
                        tint = MaterialTheme.colorScheme.onSecondaryContainer,
                    )
                }
            }

            SingleChoiceSegmentedButtonRow(modifier = Modifier.fillMaxWidth()) {
                uiState.engine.modes.forEachIndexed { index, mode ->
                    val label = mode.replaceFirstChar { current -> current.titlecase() }
                    SegmentedButton(
                        selected = uiState.engine.mode == mode,
                        onClick = { onSetMode(mode) },
                        enabled = uiState.engine.reachable && !uiState.busy,
                        shape = SegmentedButtonDefaults.itemShape(index = index, count = uiState.engine.modes.size),
                    ) {
                        Text(label)
                    }
                }
            }

            if (uiState.engine.error.isNotBlank()) {
                Text(
                    text = uiState.engine.error,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.error,
                )
            }
        }
    }
}

@Composable
private fun SelectorCard(
    selector: ProxySelectorState,
    enabled: Boolean,
    onOpen: () -> Unit,
) {
    ElevatedCard(
        onClick = onOpen,
        enabled = enabled,
        colors = CardDefaults.elevatedCardColors(
            containerColor = MaterialTheme.colorScheme.surface,
        ),
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(18.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Column(modifier = Modifier.weight(1f)) {
                    Text(selector.name, style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold)
                    Text(
                        selector.type.replaceFirstChar { it.uppercase() },
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                FilledActionChip("Select", Icons.Rounded.Sync, onOpen)
            }
            Text(
                text = selector.current.ifBlank { "Not selected" },
                style = MaterialTheme.typography.headlineSmall,
                fontWeight = FontWeight.Medium,
            )
            Text(
                text = "${selector.options.size} candidate nodes",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

@Composable
private fun SubscriptionItem(
    profile: SubscriptionProfile,
    active: Boolean,
    onSelect: () -> Unit,
    onEdit: () -> Unit,
    onDelete: () -> Unit,
) {
    val containerColor = if (active) {
        MaterialTheme.colorScheme.primaryContainer.copy(alpha = 0.42f)
    } else {
        MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.28f)
    }

    Surface(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(22.dp))
            .clickable(onClick = onSelect),
        color = containerColor,
        tonalElevation = if (active) 2.dp else 0.dp,
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Column(modifier = Modifier.weight(1f)) {
                    Text(profile.name, style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold)
                    Text(
                        profile.url,
                        maxLines = 2,
                        overflow = TextOverflow.Ellipsis,
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                if (active) {
                    StatusPill("Active")
                }
            }

            Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                TextButton(onClick = onEdit) {
                    Icon(Icons.Rounded.Edit, contentDescription = null)
                    Spacer(Modifier.size(8.dp))
                    Text("Edit")
                }
                TextButton(onClick = onDelete) {
                    Icon(Icons.Rounded.Delete, contentDescription = null)
                    Spacer(Modifier.size(8.dp))
                    Text("Delete")
                }
            }
        }
    }
}

@Composable
private fun SubscriptionEditorDialog(
    initialState: SubscriptionEditorState,
    onDismiss: () -> Unit,
    onConfirm: (String?, String, String) -> Unit,
) {
    var name by rememberSaveable(initialState.id) { mutableStateOf(initialState.name) }
    var url by rememberSaveable(initialState.id) { mutableStateOf(initialState.url) }

    AlertDialog(
        onDismissRequest = onDismiss,
        confirmButton = {
            Button(onClick = { onConfirm(initialState.id, name, url) }) {
                Text(if (initialState.id == null) "Add" else "Save")
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) {
                Text("Cancel")
            }
        },
        title = {
            Text(if (initialState.id == null) "New subscription" else "Edit subscription")
        },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                OutlinedTextField(
                    modifier = Modifier.fillMaxWidth(),
                    value = name,
                    onValueChange = { name = it },
                    label = { Text("Display name") },
                    singleLine = true,
                )
                OutlinedTextField(
                    modifier = Modifier.fillMaxWidth(),
                    value = url,
                    onValueChange = { url = it },
                    label = { Text("Subscription URL") },
                    placeholder = { Text("https://example.com/subscription.yaml") },
                    minLines = 3,
                )
            }
        },
    )
}

@Composable
private fun SelectorPickerDialog(
    state: SelectorDialogState,
    onDismiss: () -> Unit,
    onSelect: (String) -> Unit,
) {
    AlertDialog(
        onDismissRequest = onDismiss,
        confirmButton = {},
        dismissButton = {
            TextButton(onClick = onDismiss) {
                Text("Close")
            }
        },
        title = {
            Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                Text(state.groupName)
                if (state.current.isNotBlank()) {
                    Text(
                        text = "Current: ${state.current}",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        },
        text = {
            LazyColumn(
                modifier = Modifier
                    .fillMaxWidth()
                    .heightIn(max = 360.dp),
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                items(state.options) { option ->
                    Surface(
                        modifier = Modifier
                            .fillMaxWidth()
                            .clip(RoundedCornerShape(18.dp))
                            .clickable { onSelect(option) },
                        color = if (option == state.current) {
                            MaterialTheme.colorScheme.primaryContainer
                        } else {
                            MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.34f)
                        },
                    ) {
                        Row(
                            modifier = Modifier
                                .fillMaxWidth()
                                .padding(horizontal = 16.dp, vertical = 14.dp),
                            horizontalArrangement = Arrangement.SpaceBetween,
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Text(option, modifier = Modifier.weight(1f))
                            if (option == state.current) {
                                Icon(Icons.Rounded.CheckCircle, contentDescription = null)
                            }
                        }
                    }
                }
            }
        },
    )
}

@Composable
private fun FilledActionChip(label: String, icon: androidx.compose.ui.graphics.vector.ImageVector, onClick: () -> Unit) {
    AssistChip(
        onClick = onClick,
        label = { Text(label) },
        leadingIcon = { Icon(icon, contentDescription = null, modifier = Modifier.size(18.dp)) },
    )
}

@Composable
private fun StatusPill(text: String) {
    Surface(
        shape = CircleShape,
        color = MaterialTheme.colorScheme.onPrimaryContainer.copy(alpha = 0.1f),
    ) {
        Text(
            text = text,
            modifier = Modifier.padding(horizontal = 12.dp, vertical = 6.dp),
            style = MaterialTheme.typography.labelLarge,
            color = MaterialTheme.colorScheme.onPrimaryContainer,
            fontWeight = FontWeight.SemiBold,
        )
    }
}

@Composable
private fun InfoChip(label: String, value: String) {
    Surface(
        shape = RoundedCornerShape(18.dp),
        color = MaterialTheme.colorScheme.surface.copy(alpha = 0.65f),
    ) {
        Column(
            modifier = Modifier.padding(horizontal = 12.dp, vertical = 10.dp),
            verticalArrangement = Arrangement.spacedBy(2.dp),
        ) {
            Text(
                text = label,
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Text(
                text = value,
                style = MaterialTheme.typography.bodyMedium,
                fontWeight = FontWeight.Medium,
            )
        }
    }
}

@Composable
private fun MetadataLine(label: String, value: String) {
    Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Text(
            text = label,
            style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Text(
            text = value,
            style = MaterialTheme.typography.bodyMedium,
            maxLines = 3,
            overflow = TextOverflow.Ellipsis,
        )
    }
}

private data class SubscriptionEditorState(
    val id: String?,
    val name: String,
    val url: String,
)

private data class SelectorDialogState(
    val groupName: String,
    val current: String,
    val options: List<String>,
)

private fun subscriptionEditorSaver() = androidx.compose.runtime.saveable.listSaver<SubscriptionEditorState?, Any?>(
    save = { state ->
        if (state == null) {
            emptyList()
        } else {
            listOf(state.id, state.name, state.url)
        }
    },
    restore = { values ->
        if (values.size < 3) {
            null
        } else {
            SubscriptionEditorState(
                id = values[0] as String?,
                name = values[1] as String,
                url = values[2] as String,
            )
        }
    },
)

private fun selectorDialogSaver() = androidx.compose.runtime.saveable.listSaver<SelectorDialogState?, Any?>(
    save = { state ->
        if (state == null) {
            emptyList()
        } else {
            listOf(state.groupName, state.current, state.options)
        }
    },
    restore = { values ->
        if (values.size < 3) {
            null
        } else {
            @Suppress("UNCHECKED_CAST")
            SelectorDialogState(
                groupName = values[0] as String,
                current = values[1] as String,
                options = values[2] as List<String>,
            )
        }
    },
)
