package io.dropcheck.agent

import java.io.ByteArrayOutputStream
import java.net.InetSocketAddress
import java.net.Socket

// The service returns one IP literal; allow ordinary HTTP headers and small chunk framing.
internal const val MAX_GLOBAL_IP_HEADER_BYTES = 8192
internal const val MAX_GLOBAL_IP_BODY_BYTES = 4096

internal fun readGlobalIpResponse(
    socket: Socket,
    remote: InetSocketAddress,
    request: ByteArray,
    deadlineNanos: Long,
): String {
    return withProbeDeadline(deadlineNanos, socket::close) {
        socket.connect(remote, probeTimeoutMs(deadlineNanos))
        socket.soTimeout = probeTimeoutMs(deadlineNanos)
        socket.getOutputStream().write(request)
        socket.getOutputStream().flush()
        val output = ByteArrayOutputStream()
        val buffer = ByteArray(1024)
        val limit = MAX_GLOBAL_IP_HEADER_BYTES + MAX_GLOBAL_IP_BODY_BYTES
        while (true) {
            socket.soTimeout = probeTimeoutMs(deadlineNanos)
            val count = socket.getInputStream().read(buffer)
            if (count < 0) break
            check(count <= limit - output.size()) { "global IP response exceeded $limit bytes" }
            output.write(buffer, 0, count)
        }
        output.toString(Charsets.UTF_8.name())
    }
}

internal data class GlobalIpHttpResponse(val status: Int, val body: String)

internal fun parseGlobalIpHttpResponse(response: String): GlobalIpHttpResponse {
    val separator = if ("\r\n\r\n" in response) "\r\n\r\n" else "\n\n"
    val index = response.indexOf(separator)
    require(index >= 0) { "global IP response missing HTTP headers" }
    val headerText = response.take(index)
    val body = response.drop(index + separator.length)
    require(headerText.toByteArray().size <= MAX_GLOBAL_IP_HEADER_BYTES) { "global IP headers too large" }
    require(body.toByteArray().size <= MAX_GLOBAL_IP_BODY_BYTES) { "global IP body too large" }
    val lines = headerText.lineSequence().toList()
    val status = Regex("""^HTTP/\S+\s+(\d{3})(?:\s|$)""").find(lines.first())?.groupValues?.get(1)?.toInt()
        ?: throw IllegalArgumentException("invalid global IP HTTP status")
    val headers = lines.drop(1).map {
        require(':' in it) { "invalid global IP HTTP header" }
        it.substringBefore(':').trim().lowercase(java.util.Locale.US) to it.substringAfter(':').trim()
    }
    val encodings = headers.filter { it.first == "transfer-encoding" }.map { it.second }
    val lengths = headers.filter { it.first == "content-length" }.map { it.second }
    require(encodings.size <= 1 && lengths.size <= 1 && !(encodings.isNotEmpty() && lengths.isNotEmpty())) {
        "ambiguous global IP response framing"
    }
    if (encodings.isNotEmpty()) {
        require(encodings.single().equals("chunked", ignoreCase = true)) { "unsupported global IP transfer encoding" }
        return GlobalIpHttpResponse(status, decodeGlobalIpChunks(body))
    }
    if (lengths.isNotEmpty()) {
        require(lengths.single().toIntOrNull() == body.toByteArray().size) { "invalid global IP content length" }
    }
    return GlobalIpHttpResponse(status, body)
}

private fun decodeGlobalIpChunks(body: String): String {
    val output = StringBuilder()
    var cursor = 0
    while (true) {
        val lineEnd = body.indexOf('\n', cursor)
        require(lineEnd >= 0) { "incomplete global IP chunk size" }
        val size = body.substring(cursor, lineEnd).substringBefore(';').trim().toIntOrNull(16)
        require(size != null && size >= 0) { "invalid global IP chunk size" }
        cursor = lineEnd + 1
        if (size == 0) {
            val trailers = body.substring(cursor)
            require(trailers.endsWith('\n') && trailers.lineSequence().all { it.isBlank() || ':' in it }) {
                "invalid global IP chunk trailers"
            }
            return output.toString()
        }
        require(size <= body.length - cursor) { "incomplete global IP chunk" }
        output.append(body.substring(cursor, cursor + size))
        cursor += size
        cursor += when {
            body.startsWith("\r\n", cursor) -> 2
            body.startsWith("\n", cursor) -> 1
            else -> throw IllegalArgumentException("invalid global IP chunk delimiter")
        }
    }
}
