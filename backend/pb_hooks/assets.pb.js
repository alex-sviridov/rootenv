
// ─── After attempt create, start provisioning assets for the session
onRecordAfterCreateSuccess((e) => {
    const attempt = e.record
    const assetCollection = $app.findCollectionByNameOrId("assets")
    const environment = JSON.parse(attempt.get("environment"))
    const duration = environment.duration || 60
    if (environment.assets) {
        environment.assets.forEach((assetDef) => {
            console.log(`[attempt:${attempt.id}] creating asset: ${assetDef.name}`)
            const asset = new Record(assetCollection)
            asset.set("attempt", attempt.id)
            asset.set("connection", "")
            asset.set("configuration", JSON.stringify(assetDef))
            asset.set("name", assetDef.name)
            asset.set("platform", assetDef.platform)
            asset.set("state", "pending")
            asset.set("status", "poweredoff")
            asset.set("expires_at", new Date(Date.now() + duration * 60 * 1000).toISOString())
            $app.save(asset)

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
    const connection = JSON.stringify({ user: "user", host: "hostname.example.com", port: 22 })
    const asset = e.record

    // if decommissioned, check if all attempt assets are decommissioned and set attempt.finished
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