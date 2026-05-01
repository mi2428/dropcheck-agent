package io.dropcheck.agent

import io.dropcheck.agent.grpc.CommandResult
import io.dropcheck.agent.grpc.ConnectWifi
import io.dropcheck.agent.grpc.DnsAnswer
import io.dropcheck.agent.grpc.DnsRecordType
import io.dropcheck.agent.grpc.GlobalIp
import io.dropcheck.agent.grpc.GlobalIpAddress
import io.dropcheck.agent.grpc.GlobalIpResult
import io.dropcheck.agent.grpc.HttpCheck
import io.dropcheck.agent.grpc.HttpCheckResult
import io.dropcheck.agent.grpc.IpFamily
import io.dropcheck.agent.grpc.PathMtu
import io.dropcheck.agent.grpc.PathMtuResult
import io.dropcheck.agent.grpc.ResolveDns
import io.dropcheck.agent.grpc.ResolveDnsResult
import io.dropcheck.agent.grpc.RunCommand
import io.dropcheck.agent.grpc.Traceroute
import io.dropcheck.agent.grpc.TracerouteResult
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class StructuredLogTest {
    @Test
    fun rendersLogfmtStyleFieldsWithEscaping() {
        val line = StructuredLog.format(
            "grpc.rx",
            "url" to "https://www.wide.ad.jp/",
            "message" to "hello world",
            "empty" to "",
            "count" to 3,
        )

        assertTrue(line.contains("event=grpc.rx"))
        assertTrue(line.contains("url=https://www.wide.ad.jp/"))
        assertTrue(line.contains("message=\"hello world\""))
        assertTrue(line.contains("empty=\"\""))
        assertTrue(line.contains("count=3"))
    }

    @Test
    fun commandFieldsIncludeConcreteHttpDnsTraceroutePathMtuAndGlobalIpTargets() {
        val http = RunCommand.newBuilder()
            .setLabel("test http https://www.wide.ad.jp/ expected-status 301")
            .setHttpCheck(HttpCheck.newBuilder()
                .setUrl("https://www.wide.ad.jp/")
                .setExpectedStatus(301)
                .setTimeoutMs(2000))
            .build()
        val dns = RunCommand.newBuilder()
            .setResolveDns(ResolveDns.newBuilder()
                .setName("wide.ad.jp")
                .addQtypes(DnsRecordType.DNS_RECORD_TYPE_A)
                .setTimeoutMs(1000))
            .build()
        val traceroute = RunCommand.newBuilder()
            .setTraceroute(Traceroute.newBuilder()
                .setHost("1.1.1.1")
                .setMaxHops(2)
                .setTimeoutMs(3000))
            .build()
        val pathMtu = RunCommand.newBuilder()
            .setPathMtu(PathMtu.newBuilder()
                .setHost("1.1.1.1")
                .setMinMtuBytes(1200)
                .setMaxMtuBytes(1500)
                .setTimeoutMs(3000))
            .build()
        val globalIp = RunCommand.newBuilder()
            .setGlobalIp(GlobalIp.newBuilder()
                .setFamily(IpFamily.IP_FAMILY_IPV6)
                .setTimeoutMs(3000))
            .build()

        val httpLine = StructuredLog.format("command.received", http.logFields())
        val dnsLine = StructuredLog.format("command.received", dns.logFields())
        val tracerouteLine = StructuredLog.format("command.received", traceroute.logFields())
        val pathMtuLine = StructuredLog.format("command.received", pathMtu.logFields())
        val globalIpLine = StructuredLog.format("command.received", globalIp.logFields())

        assertTrue(httpLine.contains("url=https://www.wide.ad.jp/"))
        assertTrue(httpLine.contains("url_host=www.wide.ad.jp"))
        assertTrue(httpLine.contains("expected_status=301"))
        assertTrue(dnsLine.contains("name=wide.ad.jp"))
        assertTrue(dnsLine.contains("qtypes=DNS_RECORD_TYPE_A"))
        assertTrue(tracerouteLine.contains("host=1.1.1.1"))
        assertTrue(tracerouteLine.contains("max_hops=2"))
        assertTrue(pathMtuLine.contains("host=1.1.1.1"))
        assertTrue(pathMtuLine.contains("min_mtu_bytes=1200"))
        assertTrue(pathMtuLine.contains("max_mtu_bytes=1500"))
        assertTrue(globalIpLine.contains("family=IP_FAMILY_IPV6"))
        assertTrue(globalIpLine.contains("timeout_ms=3000"))
    }

    @Test
    fun commandFieldsDoNotExposeWifiPassphrases() {
        val command = RunCommand.newBuilder()
            .setLabel("request wifi connect Lab passphrase super-secret")
            .setConnectWifi(ConnectWifi.newBuilder()
                .setSsid("Lab")
                .setPassphrase("super-secret")
                .setSecurity(ConnectWifi.Security.SECURITY_WPA3_SAE))
            .build()

        val line = StructuredLog.format("command.received", command.logFields())

        assertFalse(line.contains("super-secret"))
        assertTrue(line.contains("label=\"request wifi connect Lab passphrase <redacted>\""))
        assertTrue(line.contains("passphrase_present=true"))
        assertTrue(line.contains("passphrase_len=12"))
    }

    @Test
    fun resultFieldsIncludeNetworkPayloadDetails() {
        val dnsResult = CommandResult.newBuilder()
            .setStatus(CommandResult.Status.STATUS_OK)
            .setResolveDns(ResolveDnsResult.newBuilder()
                .setName("wide.ad.jp")
                .addAnswers(DnsAnswer.newBuilder()
                    .setType(DnsRecordType.DNS_RECORD_TYPE_A)
                    .setAddress("203.178.136.63")))
            .build()
        val httpResult = CommandResult.newBuilder()
            .setStatus(CommandResult.Status.STATUS_FAILED)
            .setHttpCheck(HttpCheckResult.newBuilder()
                .setUrl("https://www.wide.ad.jp/")
                .setStatus(301)
                .setExpectedStatus(200)
                .setMatched(false))
            .build()
        val tracerouteResult = CommandResult.newBuilder()
            .setStatus(CommandResult.Status.STATUS_OK)
            .setTraceroute(TracerouteResult.newBuilder()
                .setHost("1.1.1.1")
                .setExecutable("ping TTL fallback")
                .setOutput(" 1  192.0.2.1  1.000 ms\n"))
            .build()
        val pathMtuResult = CommandResult.newBuilder()
            .setStatus(CommandResult.Status.STATUS_OK)
            .setPathMtu(PathMtuResult.newBuilder()
                .setHost("1.1.1.1")
                .setDiscovered(true)
                .setPathMtuBytes(1500)
                .setPayloadSizeBytes(1472)
                .setIpOverheadBytes(28))
            .build()
        val globalIpResult = CommandResult.newBuilder()
            .setStatus(CommandResult.Status.STATUS_OK)
            .setGlobalIp(GlobalIpResult.newBuilder()
                .setService("ifconfig.me")
                .setRequestedFamily(IpFamily.IP_FAMILY_IPV4)
                .addAddresses(GlobalIpAddress.newBuilder()
                    .setFamily(IpFamily.IP_FAMILY_IPV4)
                    .setIp("203.0.113.10")
                    .setGlobal(true)
                    .setStatus(200)))
            .build()

        val dnsLine = StructuredLog.format("command.result", dnsResult.logFields())
        val httpLine = StructuredLog.format("command.result", httpResult.logFields())
        val tracerouteLine = StructuredLog.format("command.result", tracerouteResult.logFields())
        val pathMtuLine = StructuredLog.format("command.result", pathMtuResult.logFields())
        val globalIpLine = StructuredLog.format("command.result", globalIpResult.logFields())

        assertTrue(dnsLine.contains("answers=DNS_RECORD_TYPE_A:203.178.136.63"))
        assertTrue(httpLine.contains("url_host=www.wide.ad.jp"))
        assertTrue(httpLine.contains("http_status=301"))
        assertTrue(tracerouteLine.contains("executable=\"ping TTL fallback\""))
        assertTrue(tracerouteLine.contains("output_preview="))
        assertTrue(pathMtuLine.contains("discovered=true"))
        assertTrue(pathMtuLine.contains("path_mtu_bytes=1500"))
        assertTrue(pathMtuLine.contains("payload_size_bytes=1472"))
        assertTrue(globalIpLine.contains("service=ifconfig.me"))
        assertTrue(globalIpLine.contains("requested_family=IP_FAMILY_IPV4"))
        assertTrue(globalIpLine.contains("addresses_count=1"))
    }
}
