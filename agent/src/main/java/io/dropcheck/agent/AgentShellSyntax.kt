package io.dropcheck.agent

/** Literal command metadata used by parsing, completion, and usage formatting. */
internal data class AgentShellSyntax(
    val path: String,
    val positionals: List<String> = emptyList(),
    val required: List<String> = emptyList(),
    val switches: List<String> = emptyList(),
    val values: List<String> = emptyList(),
    val enums: Map<String, List<String>> = emptyMap(),
    val effect: String,
    val description: String,
) {
    val parts = path.split(' ')
    val keys get() = switches + values
    val position get() = positionals.joinToString(" ") { it.uppercase() }
    fun usage(): String = (listOf(path) + positionals.map { "<${it.uppercase()}>" } + required.map { "$it <value>" } +
        switches.map { "[$it]" } + values.filterNot { it in required }.map { "[$it <value>]" }).joinToString(" ")
}
