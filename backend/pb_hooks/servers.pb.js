
onRecordAfterCreateSuccess((e) => {
    const server = e.record
    // pending > provisioning
    if (server.getString("state") === "pending") {
        const serverId = server.getString("id")
        try {
            server.set("state", "provisioning")
            $app.save(server)
            console.log(`[server:${serverId}] provisioning server`)
        } catch (err) {
            console.log(`[server:${serverId}] error moving to provisioning: ${err}`)
        }
    }

    e.next()
}, "servers")

onRecordAfterUpdateSuccess((e) => {
    const server = e.record
    // provisioning > provisioned
    if (server.getString("state") === "provisioning") {
        const serverId = server.getString("id")
        try {
            server.set("state", "provisioned")
            $app.save(server)
            console.log(`[server:${serverId}] provisioned server`)
        } catch (err) {
            console.log(`[server:${serverId}] error moving to provisioned: ${err}`)
        }
    }
    // decommissioning > decommissioned
    if (server.getString("state") === "decommissioning") {
        const serverId = server.getString("id")
        try {
            server.set("state", "decommissioned")
            $app.save(server)
            console.log(`[server:${serverId}] decommissioned server`)
        } catch (err) {
            console.log(`[server:${serverId}] error moving to decommissioned: ${err}`)
        }
    }
    // if decommissioned, check if all attempt servers are decommissioned and set attempt.finished
    if (server.getString("state") === "decommissioned") {
        const attemptId = server.getString("attempt")
        if (attemptId) {
            const attempt = $app.findRecordById("attempts", attemptId)
            let hasRemaining = false
            try {
                $app.findFirstRecordByFilter(
                    "servers",
                    "attempt = {:id} && state != 'decommissioned'",
                    { id: attemptId }
                )
                hasRemaining = true
            } catch (_) {}
            if (!hasRemaining) {
                try {
                    attempt.set("finished", new Date().toISOString())
                    $app.save(attempt)
                    console.log(`[attempt:${attemptId}] finished attempt`)
                } catch (err) {
                    console.log(`[attempt:${attemptId}] error finishing attempt: ${err}`)
                }
            }
        }
    }

    e.next()
}, "servers")
