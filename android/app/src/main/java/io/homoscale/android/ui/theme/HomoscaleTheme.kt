package io.homoscale.android.ui.theme

import android.os.Build
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.dynamicDarkColorScheme
import androidx.compose.material3.dynamicLightColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext

private val FallbackLightScheme = lightColorScheme(
    primary = Color(0xFF005A6A),
    onPrimary = Color(0xFFFFFFFF),
    primaryContainer = Color(0xFFA5EEFF),
    onPrimaryContainer = Color(0xFF001F26),
    secondary = Color(0xFF4B6268),
    onSecondary = Color(0xFFFFFFFF),
    secondaryContainer = Color(0xFFCDE7ED),
    onSecondaryContainer = Color(0xFF061F24),
    tertiary = Color(0xFF5A5C7E),
    onTertiary = Color(0xFFFFFFFF),
    tertiaryContainer = Color(0xFFE0E0FF),
    background = Color(0xFFF4FAFC),
    onBackground = Color(0xFF161D1F),
    surface = Color(0xFFF8FCFD),
    onSurface = Color(0xFF161D1F),
    surfaceVariant = Color(0xFFD8E4E8),
    onSurfaceVariant = Color(0xFF3D4A4E),
    outline = Color(0xFF6D7A7E),
)

private val FallbackDarkScheme = darkColorScheme(
    primary = Color(0xFF87D3E4),
    onPrimary = Color(0xFF003640),
    primaryContainer = Color(0xFF004E5B),
    onPrimaryContainer = Color(0xFFA5EEFF),
    secondary = Color(0xFFB2CBD1),
    onSecondary = Color(0xFF1D3439),
    secondaryContainer = Color(0xFF344B50),
    onSecondaryContainer = Color(0xFFCDE7ED),
    tertiary = Color(0xFFC1C3EB),
    onTertiary = Color(0xFF2A2E4D),
    tertiaryContainer = Color(0xFF414465),
    background = Color(0xFF0F1416),
    onBackground = Color(0xFFDEE5E7),
    surface = Color(0xFF11181A),
    onSurface = Color(0xFFDEE5E7),
    surfaceVariant = Color(0xFF3D4A4E),
    onSurfaceVariant = Color(0xFFBCC8CC),
    outline = Color(0xFF869398),
)

@Composable
fun HomoscaleTheme(content: @Composable () -> Unit) {
    val darkTheme = isSystemInDarkTheme()
    val context = LocalContext.current
    val colorScheme = when {
        Build.VERSION.SDK_INT >= Build.VERSION_CODES.S && darkTheme -> dynamicDarkColorScheme(context)
        Build.VERSION.SDK_INT >= Build.VERSION_CODES.S && !darkTheme -> dynamicLightColorScheme(context)
        darkTheme -> FallbackDarkScheme
        else -> FallbackLightScheme
    }

    MaterialTheme(
        colorScheme = colorScheme,
        content = content,
    )
}
