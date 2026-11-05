// Package watchrun is the private runtime for watch YAML and built-in Shell
// profiles. Compile freezes validated typed operations and credentials; Execute
// runs bounded retry/repeat/eventually/stability policies with ordered events,
// controls, reports, and connection cleanup. Atomic/compound acquisition and
// typed payload validation belong to internal/acquire, not this policy package.
package watchrun
