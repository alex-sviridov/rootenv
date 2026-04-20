


Based on the plan, here are natural next additions to consider:

Iteration 6 — Reconnect & Session Recovery — Store minimal session state (user + server binding) so a disconnected frontend can resume the same PTY without losing context. Useful for network flakiness on mobile.

Iteration 7 — Audit & Compliance — Structured event logging for security: all auth attempts (success/fail), privilege escalation checks, SSH command recording if needed. Feed to external log aggregation.

Iteration 8 — Performance Tuning — Benchmark I/O under load; measure and optimize WebSocket frame batching, SSH read buffer sizing, and memory footprint per connection.

SSH Key Rotation — Mechanism to rotate RELAY_PRIVATE_KEY_PATH without redeploying, plus support for multiple keys (e.g., per-lab or per-user key distribution).

Connection Lifecycle Hooks — Callbacks (logging, quota tracking, analytics) when a connection opens/closes — useful for billing or abuse detection.

