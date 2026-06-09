// ─── Before update: block regular users from writing current_state ────────────
onRecordUpdateRequest((e) => {
    const auth = e.auth
    // No auth = admin/hook context ($app.save); allow unconditionally.
    if (!auth) { e.next(); return }
    // Service accounts (contmgr, relay) may write any field via API.
    const svcRole = auth.getString ? auth.getString("svc_role") : ""
    if (svcRole === "contmgr" || svcRole === "relay") { e.next(); return }
    if (e.requestInfo().body["current_state"] !== undefined) {
        throw new ForbiddenError("current_state is read-only")
    }
    e.next()
}, "attempts")

// ─── Before create: validate, set initial states, create attempt_configs ──────
onRecordCreateRequest((e) => {
    const auth = e.auth
    if (!auth) throw new UnauthorizedError("authentication required")

    const labId = e.record.getString("lab")
    let labEnvironment = null
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
                labEnvironment = labRecords[0].get("environment")
            }
        } catch (_) {}
    }

    let active = []
    try {
        active = $app.findRecordsByFilter(
            "attempts",
            "user = {:userId} && current_state != 'decommissioned'",
            "", 1, 0,
            { userId: auth.id }
        )
    } catch (_) {}
    if (active.length > 0) {
        throw new BadRequestError("active attempt already exists: " + active[0].getString("lab_name"))
    }

    e.record.set("current_state", "new")
    e.record.set("desired_state", "provisioned")

    console.log(`[attempt:new] user=${auth.id} lab=${labId} → starting`)
    e.next()
}, "attempts")

// ─── After create: create attempt_configs and fan out assets ──────────────────
onRecordAfterCreateSuccess((e) => {
    const attempt = e.record
    let environment = null

    // Create attempt_configs (attempt is now in DB, FK constraint satisfied)
    const labId = attempt.getString("lab")
    if (labId) {
        try {
            const labRecords = $app.findRecordsByFilter("labs", "id = {:id}", "", 1, 0, { id: labId })
            if (labRecords.length > 0) {
                const labEnvironment = labRecords[0].get("environment")
                environment = JSON.parse(labRecords[0].getString("environment"))
                const attemptConfigsCol = $app.findCollectionByNameOrId("attempt_configs")
                const cfg = new Record(attemptConfigsCol)
                cfg.set("attempt", attempt.id)
                cfg.set("environment", labEnvironment)
                $app.save(cfg)
            }
        } catch (err) {
            console.log(`[attempt:${attempt.id}] error creating attempt_configs: ${err}`)
        }
    }

    if (!environment) {
        console.log(`[attempt:${attempt.id}] no environment found, skipping asset creation`)
        e.next()
        return
    }

    const duration = parseInt(environment.duration, 10) || 60
    const expiresAt = new Date(Date.now() + duration * 60 * 1000).toISOString()
    attempt.set("expires_at", expiresAt)
    $app.save(attempt)

    const assetCollection = $app.findCollectionByNameOrId("assets")
    const assetConfigCollection = $app.findCollectionByNameOrId("assets_configs")
    const keysCollection = $app.findCollectionByNameOrId("keys")

    if (environment.assets) {
        environment.assets.forEach((assetDef) => {
            console.log(`[attempt:${attempt.id}] creating asset: ${assetDef.name}`)
            const asset = new Record(assetCollection)
            asset.set("attempt", attempt.id)
            asset.set("name", assetDef.name)
            asset.set("protocols", JSON.stringify(assetDef.relay_protocols || ["none"]))
            asset.set("state", "pending")
            asset.set("user", attempt.get("user"))
            asset.set("status", "stopped")
            asset.set("expires_at", expiresAt)
            $app.save(asset)

            const cfg = new Record(assetConfigCollection)
            cfg.set("asset", asset.id)
            cfg.set("platform", assetDef.platform)
            cfg.set("configuration", JSON.stringify(assetDef))
            $app.save(cfg)

            const key = new Record(keysCollection)
            key.set("asset", asset.id)
            $app.save(key)
        })
    }

    e.next()
}, "attempts")
