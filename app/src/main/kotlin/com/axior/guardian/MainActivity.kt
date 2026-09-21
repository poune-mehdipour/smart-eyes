package com.axior.guardian

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.Surface
import androidx.compose.ui.Modifier
import com.axior.guardian.feature.monitoring.MonitoringScreen
import com.axior.guardian.ui.theme.GuardianTheme
import dagger.hilt.android.AndroidEntryPoint

@AndroidEntryPoint
class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        enableEdgeToEdge()
        super.onCreate(savedInstanceState)
        setContent {
            GuardianTheme {
                Surface(modifier = Modifier.fillMaxSize()) {
                    MonitoringScreen()
                }
            }
        }
    }
}
