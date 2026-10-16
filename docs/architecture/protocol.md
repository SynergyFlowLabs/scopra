# Scopra Messenger Protocol

Scopra Messenger is the internal, versioned request and dispatch protocol used between cloud services and the local daemon. It is distinct from the Model Context Protocol (MCP), which is an agent integration interface.

Cloud-to-Mio envelopes contain version, type, message ID, device audience, issuer, sequence, creation time, expiry, nonce, payload hash, and authentication proof.

Execution requests contain all principals, resources, grant/session IDs, executable, arguments, explicit shell flag, working directory, profiles, resource limits, output policy, reason, timestamps, and nonce.
