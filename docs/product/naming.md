# Scopra Product and Component Names

**Status:** Proposed naming map  
**Scope:** Architecture and product-facing component names

Scopra remains the umbrella product. Its components use a shared, Mercury-exploration-inspired naming system. These names identify parts of Scopra; they are not separate products.

| Name | Component | Responsibility |
|---|---|---|
| **Scopra** | Product | Controlled execution infrastructure for AI agents. |
| **Scopra Mariner** | CLI, onboarding, and enrollment | Guided CLI/TUI onboarding and machine enrollment. |
| **Scopra Mio** | Local daemon | Registers workspaces, maintains the authenticated machine connection, and coordinates local execution. |
| **Scopra Caloris** | Cloud control plane | Identity, grants, cloud policy, sessions, approvals, and control-plane APIs. |
| **Scopra Goldstone** | Relay | Secure agent-to-machine message relay. |
| **Scopra Messenger Protocol** | Internal protocol | Versioned, authenticated request and dispatch envelopes between cloud services and the local daemon. This is distinct from the Model Context Protocol (MCP). |
| **Scopra Resolution** | Policy engine | Evaluates cloud and local policy decisions. |
| **Scopra Rupes** | Local enforcement boundary | Applies local maximum policy and enforces the final authorization decision on the machine. |
| **Scopra Hollow** | Execution sandbox | Ephemeral, rootless, isolated environment for approved work. |
| **Scopra Catena** | Audit chain | Append-only chain of attributable execution and security events. |
| **Scopra Facula** | Security signals | Security signals and anomaly detection. |

## Naming and implementation notes

- Use the full name at first mention in product-facing documentation; the short component name (for example, **Mio** or **Catena**) is fine afterward.
- **Resolution** evaluates policy; **Rupes** is the local enforcement boundary. Cloud authorization may narrow local authority but must never broaden it.
- **Goldstone** is the relay service; **Messenger** is the protocol carried through the system. **Messenger** does not refer to MCP.
- The current executable names remain `scopra` for the CLI and `scoprad` for the daemon. **Mariner** and **Mio** are the component names; changing executable names, package paths, service identifiers, or wire-level identifiers requires a separate compatibility decision.
- Confirm trademark, domain, package-registry, and repository-name availability before public launch.
