
// Commands hook — decommission is now handled by contmgr reconciler (via attempt.desired_state).
// start/stop/restart commands remain here for future use.

onRecordAfterCreateSuccess((e) => {
    const command = e.record
    if (command.getString("command") === "decommission") {
        // No-op: contmgr watches attempt.desired_state and decommissions assets directly.
        // This handler is kept as a stub in case the commands queue is reused for other ops.
    }

    e.next()
}, "commands")
