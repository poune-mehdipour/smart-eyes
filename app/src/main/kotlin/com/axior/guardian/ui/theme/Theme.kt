package com.axior.guardian.ui.theme

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable

private val DarkColors = darkColorScheme(
    primary = GuardianGreen,
    error = GuardianRed,
    tertiary = GuardianAmber,
    surface = Surface,
    onSurface = OnSurface,
)

private val LightColors = lightColorScheme(
    primary = GuardianGreen,
    error = GuardianRed,
    tertiary = GuardianAmber,
)

@Composable
fun GuardianTheme(
    darkTheme: Boolean = isSystemInDarkTheme(),
    content: @Composable () -> Unit,
) {
    MaterialTheme(
        colorScheme = if (darkTheme) DarkColors else LightColors,
        typography = GuardianTypography,
        content = content,
    )
}
