package io.dropcheck.agent

import android.net.wifi.ScanResult
import io.dropcheck.agent.grpc.WifiBand
import java.nio.ByteBuffer
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Test

class WifiDiagnosticNamesTest {
    @Test
    fun formatsFrequencyBands() {
        assertEquals("2.4GHz", bandNameForFrequency(2412))
        assertEquals("5GHz", bandNameForFrequency(5180))
        assertEquals("6GHz", bandNameForFrequency(5975))
        assertEquals("60GHz", bandNameForFrequency(60480))
        assertEquals("unknown", bandNameForFrequency(8000))
    }

    @Test
    fun formatsRequestedWifiBands() {
        assertEquals("all", wifiBandName(WifiBand.WIFI_BAND_ALL))
        assertEquals("2.4GHz", wifiBandName(WifiBand.WIFI_BAND_2_4_GHZ))
        assertEquals("5GHz", wifiBandName(WifiBand.WIFI_BAND_5_GHZ))
        assertEquals("6GHz", wifiBandName(WifiBand.WIFI_BAND_6_GHZ))
        assertEquals("60GHz", wifiBandName(WifiBand.WIFI_BAND_60_GHZ))
    }

    @Test
    fun formatsChannelWidths() {
        assertEquals("20MHz", channelWidthName(ScanResult.CHANNEL_WIDTH_20MHZ))
        assertEquals("40MHz", channelWidthName(ScanResult.CHANNEL_WIDTH_40MHZ))
        assertEquals("80MHz", channelWidthName(ScanResult.CHANNEL_WIDTH_80MHZ))
        assertEquals("160MHz", channelWidthName(ScanResult.CHANNEL_WIDTH_160MHZ))
        assertEquals("320MHz", channelWidthName(ScanResult.CHANNEL_WIDTH_320MHZ))
    }

    @Test
    fun formatsWifiInfoIpv4LittleEndianAddress() {
        assertEquals("192.168.1.1", formatIpv4(0x0101A8C0))
        assertEquals("", formatIpv4(0))
    }

    @Test
    fun copiesRemainingByteBufferBytes() {
        val buffer = ByteBuffer.wrap(byteArrayOf(1, 2, 3, 4))
        buffer.get()

        assertArrayEquals(byteArrayOf(2, 3, 4), byteBufferBytes(buffer))
        assertEquals("020304", byteBufferBytes(buffer).toHex())
    }
}
