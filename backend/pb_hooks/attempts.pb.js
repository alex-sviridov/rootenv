// ─── Before create: check active attempts, populate lab_name and environment from labs collection ─────────────────────────────
onRecordCreateRequest((e) => {
    const auth = e.auth
    if (!auth) throw new UnauthorizedError("authentication required")

    const labId = e.record.getString("lab")
    if (labId) {
        try {
            const labRecords = $app.findRecordsByFilter(
                "labs",
                "id = {:id}",
                "",
                1,
                0,
                { id: labId }
            )
            if (labRecords.length > 0) {
                e.record.set("lab_name", labRecords[0].getString("title"))
                e.record.set("environment", labRecords[0].get("environment"))
            }
        } catch (_) {}
    }

    let active = []
    try {
        active = $app.findRecordsByFilter(
            "attempt_userview",
            "user = {:userId} && state != 'decommissioned'",
            "", 1, 0,
            { userId: auth.id }
        )
    } catch (_) {}
    if (active.length > 0) {
        throw new BadRequestError("active attempt already exists: " + active[0].getString("lab_name"))
    }

    console.log(`[session:new] user=${auth.id} lab=${e.record.getString("lab")} → starting`)
    e.next()
}, "attempts")


// ─── After create, start provisioning servers for the session
onRecordAfterCreateSuccess((e) => {
    const attempt = e.record
    const serverCollection = $app.findCollectionByNameOrId("servers")
    const environment = JSON.parse(attempt.get("environment"))
    if (environment.servers) {
        environment.servers.forEach((serverDef) => {
            console.log(`[attempt:${attempt.id}] creating server: ${serverDef.name}`)
            const server = new Record(serverCollection)
            server.set("attempt", attempt.id)
            server.set("name", serverDef.name)
            server.set("state", "pending")
            server.set("status", "poweredoff")
            $app.save(server)
        })
    }
    
    e.next()
}, "attempts")
