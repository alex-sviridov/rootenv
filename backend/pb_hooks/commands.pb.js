
// Watches the commands queue and executes server lifecycle operations.
onRecordAfterCreateSuccess((e) => {
    const command = e.record
    // decommission (sets server state → "decommissioning" and marks command done).
    if (command.getString("command") === "decommission") {
        const assetId = command.getString("asset")
        try {
            const asset = $app.findRecordById("assets", assetId)
            const state = asset.getString("state")
            if (state !== "decommissioning" && state !== "decommissioned") {
                asset.set("state", "decommissioning")
                $app.save(asset)
                console.log(`[asset:${assetId}] state → decommissioning`)
            }
            // leave command status=pending so contmgr picks it up, removes the container, then marks done
        } catch (err) {
            console.log(`[asset:${assetId}] decommission error: ${err}`)
            command.set("status", "error")
            $app.save(command)
        }
    }

    e.next()
}, "commands")
