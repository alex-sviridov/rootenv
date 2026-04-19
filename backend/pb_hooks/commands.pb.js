
// Watches the commands queue and executes server lifecycle operations.
onRecordAfterCreateSuccess((e) => {
    const command = e.record
    // decommission (sets server state → "decommissioning" and marks command done).
    if (command.getString("command") === "decommission") {
        const serverId = command.getString("server")
        try {
            const serverRecord = $app.findRecordById("servers", serverId)
            const state = serverRecord.getString("state")
            if (state === "decommissioning" || state === "decommissioned") {
                command.set("status", "done")
                $app.save(command)
            } else {
                try {
                    console.log(`[server:${serverId}] decommissioning server (current state: ${state})`)
                    serverRecord.set("state", "decommissioning")
                    $app.save(serverRecord)
                    command.set("status", "done")
                    $app.save(command)
                } catch (err) {
                    console.log(`[server:${serverId}] decommission error: ${err}`)
                    command.set("status", "error")
                    $app.save(command)
                }
            }
        } catch (err) {
            console.log(`[server:${serverId}] decommission error: ${err}`)
            command.set("status", "error")
            $app.save(command)
        }
    }

    e.next()
}, "commands")
