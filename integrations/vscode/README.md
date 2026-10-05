# VS Code guidance for the DevCadence Principal

Guidance and samples only. WP-M5-4 does not generate or apply VS Code configuration, and nothing here is first-class empirical readiness: no VS Code version, OS or session has been verified.

Use the same no-argument stdio server as every other host:

- Portable `.mcp.json`: [mcp.portable.json](mcp.portable.json) (root `mcpServers`).
- Native `.vscode/mcp.json`: [mcp.native.json](mcp.native.json) (root `servers`).

Replace the three placeholders with absolute paths and your project id. `DEVCADENCE_HOME` is required and must be an absolute, private (`0700`, owner-owned) directory outside the target source. To use a nondefault binding file add the env pair `"DEVCADENCE_PRINCIPAL_BINDING": "/absolute/private/principal-binding.json"`; otherwise the server reads `$DEVCADENCE_HOME/config/principal-binding.json`. No `args` are needed. The project id is configuration, not authority: what the host may do is decided by the protected binding and server-side policy.

Caveats:

- When VS Code runs an Agent Host, check that the MCP server definition and environment are forwarded to it; confirm in a real session.
- VS Code's optional MCP process sandbox is supported on macOS and Linux only. It is not supported on Windows, and this guidance does not rely on it.
- Tool approval controls whether the host may invoke a tool. It is not source isolation and it is not DevCadence authority.
- Readiness claims are scoped to the tested host version, OS and session. Strict readiness needs independently tested negative permission evidence; samples prove nothing about it.
