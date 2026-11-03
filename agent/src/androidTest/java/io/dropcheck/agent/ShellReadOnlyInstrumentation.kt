package io.dropcheck.agent

import android.app.Activity
import android.app.Instrumentation
import android.content.Intent
import android.content.pm.ActivityInfo
import android.content.pm.PackageManager
import android.content.res.Configuration
import android.os.Build
import android.os.Bundle
import android.os.SystemClock
import android.view.KeyEvent
import android.view.inputmethod.EditorInfo
import android.widget.EditText
import io.dropcheck.agent.grpc.CommandLog
import io.dropcheck.agent.grpc.CommandResult
import io.dropcheck.agent.grpc.GetIpStatus
import io.dropcheck.agent.grpc.RunCommand
import org.json.JSONObject

/** Real input/async execution/presentation; no scan refresh, mutation or external probe. */
class ShellReadOnlyInstrumentation : Instrumentation() {
    private var current: MainActivity? = null
    private var phase = "startup"
    private var cases = 0

    override fun onCreate(arguments: Bundle?) { super.onCreate(arguments); start() }

    override fun onStart() {
        val report = Bundle()
        var session: AgentShellSession? = null
        var dualOutcome = "NOT_MEASURED"
        var dualFailedMetrics = "NOT_MEASURED"
        var lanPermission = "NOT_APPLICABLE"
        try {
            current = startActivitySync(Intent(targetContext, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)) as MainActivity
            waitForIdleSync()
            val permissions = json("show permissions | display json")
            check(permissions.getBoolean("ok") && lines(permissions).any { it.startsWith("Device Owner:") }) { "permission snapshot unavailable" }
            if (Build.VERSION.SDK_INT >= 37) {
                lanPermission = if (targetContext.checkSelfPermission("android.permission.ACCESS_LOCAL_NETWORK") == PackageManager.PERMISSION_GRANTED) "granted" else "denied"
                check("ACCESS_LOCAL_NETWORK: $lanPermission" in lines(permissions)) { "LAN permission display differs from platform state" }
            }
            cases++
            val networks = json("show network | display json")
            check(networks.getBoolean("ok")) { "network snapshot unavailable" }
            val knownIds = ids(networks)
            check(knownIds.isNotEmpty()) { "no Network available for default/identity acceptance" }
            cases++
            val default = json("show network default | display json")
            check(default.getBoolean("ok") && ids(default).size == 1) { "default Network unavailable" }
            val id = ids(default).single()
            check(validNetworkId(id)) { "noncanonical Network identity" }
            val selected = json("show network id $id | display json")
            check(selected.getBoolean("ok") && ids(selected) == listOf(id)) { "explicit Network identity changed" }
            cases++
            val stale = (Int.MAX_VALUE downTo Int.MAX_VALUE - 100).first { it.toString() !in knownIds }.toString()
            val absent = json("show network id $stale | display json")
            check(!absent.getBoolean("ok") && ids(absent).isEmpty()) { "stale Network silently fell back" }
            check(submit("show network id $stale | match acceptance-no-output-3a7f").contains("Command FAILED (output filtered)")) { "filtered failure disappeared" }
            cases++
            check(Regex("Count: [1-9][0-9]* lines").containsMatchIn(submit("show network | find '^Network:' | match '^Network:' | count"))) { "logical pipeline count failed" }
            check(submit("show permissions | display json | count").contains("cannot be combined")) { "unsafe JSON combination accepted" }
            cases++
            phase = "link family all"
            val quiet = object : CommandLogger { override fun log(level: CommandLog.Level, message: String, scope: CommandLogScope) = Unit }
            val ip = CommandExecutor(targetContext.applicationContext, quiet).execute(RunCommand.newBuilder().setGetIpStatus(GetIpStatus.getDefaultInstance()).build())
            check(ip.status == CommandResult.Status.STATUS_OK && ip.hasIpStatus() && ip.ipStatus.hasWifi() && isKnownWifiSsid(ip.ipStatus.wifi.ssid)) { "physical Wi-Fi identity unavailable for dual-family check" }
            val ssid = ip.ipStatus.wifi.ssid.replace("\\", "\\\\").replace("\"", "\\\"")
            val dual = json("test link ssid \"$ssid\" family all | display json")
            val findings = lines(dual)
            check(listOf("ipv4.addresses:", "ipv4.default_routes:", "ipv4.dns_servers:", "ipv6.addresses:", "ipv6.default_routes:", "ipv6.dns_servers:").all { metric -> findings.any { metric in it } }) { "dual-family findings missing" }
            check(dual.getBoolean("ok") || findings.any { ": FAIL" in it || ": MISSING" in it }) { "dual-family outcome inconsistent" }
            dualOutcome = if (dual.getBoolean("ok")) "PASS" else "NOT_PASS_WITH_EXPLICIT_FINDINGS"
            dualFailedMetrics = findings.mapNotNull { Regex("(ipv[46]\\.(?:addresses|default_routes|dns_servers)): (?:FAIL|MISSING)").find(it)?.groupValues?.get(1) }.distinct().joinToString(",").ifEmpty { "none" }
            cases++
            check(submit("show permissions").contains("Device Owner:")) { "unfiltered permission output unavailable" }
            submit("set cli default-passphrase acceptance-only-psk")
            runOnMainSync {
                session = field(current!!, "shellSession")
                check(session!!.defaults.defaultPassphrase == "acceptance-only-psk") { "session default not set" }
                check(!field<AgentBlockHistory>(current!!, "shellTranscript").blocks.any { "acceptance-only-psk" in it.fullText }) { "credential entered display history" }
            }
            cases++
            phase = "landscape recreation"
            val monitor = addMonitor(MainActivity::class.java.name, null, false)
            val previous = current!!
            try {
                runOnMainSync { previous.requestedOrientation = ActivityInfo.SCREEN_ORIENTATION_LANDSCAPE }
                current = monitor.waitForActivityWithTimeout(5000) as? MainActivity ?: error("landscape Activity did not resume")
            } finally { removeMonitor(monitor) }
            waitForIdleSync()
            runOnMainSync {
                check(current !== previous && current!!.resources.configuration.orientation == Configuration.ORIENTATION_LANDSCAPE) { "Activity did not rotate" }
                check(field<AgentShellSession>(current!!, "shellSession") === session && session!!.defaults.defaultPassphrase == "acceptance-only-psk") { "session lost on rotation" }
                val input = field<EditText>(current!!, "shellInput")
                check(input.text.isEmpty() && !input.isSaveEnabled) { "input was persisted across recreation" }
                input.requestFocus()
                input.dispatchKeyEvent(KeyEvent(KeyEvent.ACTION_DOWN, KeyEvent.KEYCODE_DPAD_UP))
                check(input.text.toString() == "show permissions") { "credential or filtered command entered recall history" }
                input.dispatchKeyEvent(KeyEvent(KeyEvent.ACTION_DOWN, KeyEvent.KEYCODE_DPAD_DOWN))
                check(input.text.isEmpty()) { "history draft not restored" }
            }
            cases++
            submit("clear cli default-passphrase")
            runOnMainSync { check(session!!.defaults.defaultPassphrase.isEmpty()) { "session default not cleared" } }
            check(json("show network default | display json").getBoolean("ok")) { "post-rotation read failed" }
            cases++
            report.putString("stream", "PASS: actual IME submit/async Shell results cases=$cases; target_sdk=${targetContext.applicationInfo.targetSdkVersion} runtime_lan=$lanPermission; Networks/default/explicit/stale IDs, permissions, pipelines/JSON, filtered failure, dual_link=$dualOutcome dual_failed_metrics=$dualFailedMetrics, session credential/privacy/history and landscape recreation; networks=${knownIds.size}; mutation=0 fresh_scan=0 external_probe=0; private identities omitted.\n")
            finish(Activity.RESULT_OK, report)
        } catch (failure: Throwable) {
            report.putString("stream", "FAIL: stage=$phase cases=$cases ${failure.javaClass.simpleName}: ${failure.message}\n")
            finish(Activity.RESULT_CANCELED, report)
        } finally {
            session?.setPassphrase("")
            current?.let { activity -> runOnMainSync { activity.requestedOrientation = ActivityInfo.SCREEN_ORIENTATION_UNSPECIFIED; activity.finish() } }
        }
    }

    private fun submit(line: String): String {
        check(line.startsWith("show network ") || line.startsWith("show permissions") ||
            line.startsWith("test link ssid ") ||
            line == "set cli default-passphrase acceptance-only-psk" || line == "clear cli default-passphrase") { "read-only/session command boundary" }
        phase = when { line.startsWith("show network ") -> "Network selection/pipeline"; line.startsWith("test link ") -> "link family all"; else -> "permissions/session" }
        val activity = current!!
        var previous: AgentPresentationBlock? = null
        runOnMainSync {
            previous = field<AgentBlockHistory>(activity, "shellTranscript").blocks.lastOrNull()
            val input = field<EditText>(activity, "shellInput")
            input.setText(""); input.requestFocus()
            val connection = checkNotNull(input.onCreateInputConnection(EditorInfo()))
            check(connection.commitText(line, 1) && connection.performEditorAction(EditorInfo.IME_ACTION_GO)) { "IME submission failed" }
            check(input.text.isEmpty()) { "IME Go did not submit" }
        }
        val deadline = SystemClock.elapsedRealtime() + 10000
        while (SystemClock.elapsedRealtime() < deadline) {
            var output: String? = null
            runOnMainSync {
                val last = field<AgentBlockHistory>(activity, "shellTranscript").blocks.lastOrNull()
                if (!field<ShellActionOwner>(activity, "shellOwner").busy && last != null && last !== previous) output = last.fullText
            }
            if (output != null) return output!!
            Thread.sleep(25)
        }
        error("Shell operation did not finish within 10 seconds")
    }

    private fun json(line: String) = JSONObject(submit(line))
    private fun lines(value: JSONObject): List<String> = value.getJSONArray("lines").let { rows -> (0 until rows.length()).map { rows.getString(it) } }
    private fun ids(value: JSONObject) = lines(value).filter { it.startsWith("Network: ") }.map { it.removePrefix("Network: ").trim() }
    @Suppress("UNCHECKED_CAST") private fun <T> field(activity: MainActivity, name: String): T = MainActivity::class.java.getDeclaredField(name).apply { isAccessible = true }.get(activity) as T
}
