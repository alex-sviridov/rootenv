
// ─── After attempt create, start provisioning assets for the session
onRecordAfterCreateSuccess((e) => {
    const attempt = e.record
    const assetCollection = $app.findCollectionByNameOrId("assets")
    const assetConfigCollection = $app.findCollectionByNameOrId("assets_configs")
    const environment = JSON.parse(attempt.get("environment"))
    const duration = environment.duration || 60
    if (environment.assets) {
        environment.assets.forEach((assetDef) => {
            console.log(`[attempt:${attempt.id}] creating asset: ${assetDef.name} for user ${attempt.get("user")} with duration ${duration} minutes`)
            const asset = new Record(assetCollection)
            asset.set("attempt", attempt.id)
            asset.set("name", assetDef.name)
            asset.set("protocols", JSON.stringify(assetDef.relay_protocols || ["none"]))
            asset.set("state", "pending")
            asset.set("user", attempt.get("user"))
            asset.set("status", "poweredoff")
            asset.set("expires_at", new Date(Date.now() + duration * 60 * 1000).toISOString())
            $app.save(asset)

            const cfg = new Record(assetConfigCollection)
            cfg.set("asset", asset.id)
            cfg.set("platform", assetDef.platform)
            cfg.set("configuration", JSON.stringify(assetDef))
            $app.save(cfg)

            const keysCollection = $app.findCollectionByNameOrId("keys")
            const key = new Record(keysCollection)
            key.set("asset", asset.id)
            $app.save(key)
        })
    }

    e.next()
}, "attempts")


// after asset is updated, check if attempt is finished
onRecordAfterUpdateSuccess((e) => {
    const asset = e.record

    if (asset.getString("state") === "decommissioned") {
        const attemptId = asset.getString("attempt")
        if (attemptId) {
            const attempt = $app.findRecordById("attempts", attemptId)
            let hasRemaining = false
            try {
                $app.findFirstRecordByFilter(
                    "assets",
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
}, "assets")

// ─── Cron: create decommission commands for expired assets ───────────────────
cronAdd("decommission-expired-assets", "* * * * *", () => {
    const expiredAssets = $app.findRecordsByFilter(
        "assets",
        "expires_at != '' && expires_at <= @now && state != 'decommissioning' && state != 'decommissioned'",
        "", 0, 0,
    )

    const commandsCollection = $app.findCollectionByNameOrId("commands")
    for (const asset of expiredAssets) {
        try {
            const cmd = new Record(commandsCollection)
            cmd.set("asset", asset.id)
            cmd.set("command", "decommission")
            cmd.set("status", "pending")
            $app.save(cmd)
            console.log(`[cron] asset ${asset.id} expired → decommission command created`)
        } catch (err) {
            console.log(`[cron] error creating decommission command for asset ${asset.id}: ${err}`)
        }
    }
})
