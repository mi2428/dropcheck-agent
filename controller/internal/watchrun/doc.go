// Package watchrun is the private runtime for watch YAML and built-in Shell
// profiles. Compile freezes validated typed operations and credentials; Execute
// runs bounded retry/repeat/eventually/stability policies with ordered events,
// controls, reports, and connection cleanup. ExecuteOperation is also the shared
// atomic/compound acquisition boundary used by the Shell.
package watchrun
