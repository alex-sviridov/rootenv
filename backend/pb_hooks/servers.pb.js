
// ─── After attempt create, start provisioning servers for the session
onRecordAfterCreateSuccess((e) => {
    const attempt = e.record
    const serverCollection = $app.findCollectionByNameOrId("servers")
    const environment = JSON.parse(attempt.get("environment"))
    const duration = environment.duration || 60
    if (environment.servers) {
        environment.servers.forEach((serverDef) => {
            console.log(`[attempt:${attempt.id}] creating server: ${serverDef.name}`)
            const server = new Record(serverCollection)
            server.set("attempt", attempt.id)
            server.set("name", serverDef.name)
            server.set("state", "pending")
            server.set("status", "poweredoff")
            server.set("expires_at", new Date(Date.now() + duration * 60 * 1000).toISOString())
            $app.save(server)
        })
    }
    
    e.next()
}, "attempts")

// after server is created, move it to provisioning 

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

// after server is updated, check if we need to move it to provisioned or decommissioned, and if decommissioned check if attempt is finished
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

// ─── Cron: stop server with expired duration, if not already decommissioned ─────────────────────────────
cronAdd("decommission-expired-servers", "* * * * *", () => {
    const now = new Date().toISOString()
    console.log(`[cron] expire-servers tick, now=${now}`)
    const expiredServers = $app.findRecordsByFilter(
        "servers",
        "expires_at != '' && expires_at <= @now && state != 'decommissioned'",
        "", 0, 0,
    )

    console.log(`[cron] matched ${expiredServers.length} server(s)`)
    for (const server of expiredServers) {
        const serverId = server.id
        console.log(`[cron] server ${serverId}: state=${server.getString("state")}, expires_at=${server.getString("expires_at")}`)
        try {
            server.set("state", "decommissioning")
            $app.save(server)
            console.log(`[cron] server ${serverId} expired → decommissioning`)
        } catch (err) {
            console.log(`[cron] error expiring server ${serverId}: ${err}`)
        }
    }
})