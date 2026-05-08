package io.dropcheck.agent

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ClockWidgetWifiPolicyTest {
    @Test
    fun treatsHotspotPlaceholderWifiInfoAsDisconnected() {
        assertFalse(
            clockWidgetWifiInfoIsUsable(
                networkId = -1,
                ssid = "<unknown ssid>",
                bssid = "02:00:00:00:00:00",
                supplicantState = "DISCONNECTED",
            ),
        )
        assertFalse(
            clockWidgetWifiInfoIsUsable(
                networkId = -1,
                ssid = null,
                bssid = null,
                supplicantState = null,
            ),
        )
    }

    @Test
    fun acceptsRealClientWifiIdentity() {
        assertTrue(
            clockWidgetWifiInfoIsUsable(
                networkId = -1,
                ssid = "\"Lab\"",
                bssid = "02:00:00:00:00:00",
                supplicantState = "DISCONNECTED",
            ),
        )
        assertTrue(
            clockWidgetWifiInfoIsUsable(
                networkId = -1,
                ssid = "<unknown ssid>",
                bssid = "aa:bb:cc:dd:ee:ff",
                supplicantState = "DISCONNECTED",
            ),
        )
    }

    @Test
    fun acceptsCompletedFrameworkConnectionWhenIdentityIsRedacted() {
        assertTrue(
            clockWidgetWifiInfoIsUsable(
                networkId = 7,
                ssid = "<unknown ssid>",
                bssid = "02:00:00:00:00:00",
                supplicantState = "COMPLETED",
            ),
        )
    }

    @Test
    fun hidesLocalOnlyWifiNetworksEvenWhenIdentityIsPresent() {
        assertFalse(clockWidgetWifiNetworkIsDisplayable(localNetwork = true, wifiInfoUsable = true))
        assertFalse(clockWidgetWifiNetworkIsDisplayable(localNetwork = false, wifiInfoUsable = false))
        assertTrue(clockWidgetWifiNetworkIsDisplayable(localNetwork = false, wifiInfoUsable = true))
    }
}
