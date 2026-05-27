
// ─── After attempt create, start provisioning assets for the session
// (logic moved to attempts.pb.js after-create hook)

// ─── After asset update: recompute attempt current_state from all assets ───────
onRecordAfterUpdateSuccess((e) => {
    const attemptId = e.record.getString("attempt")
    if (!attemptId) { e.next(); return }

    const allAssets = $app.findRecordsByFilter("assets", "attempt = {:id}", "", 0, 0, { id: attemptId })
    let state = "new"
    if (allAssets.length > 0) {
        const states = allAssets.map(a => a.getString("state"))
        if (states.every(s => s === "decommissioned"))                          state = "decommissioned"
        else if (states.some(s => s === "decommissioning"))                     state = "decommissioning"
        else if (states.every(s => s === "provisioned"))                        state = "provisioned"
        else if (states.some(s => s === "provisioning" || s === "pending"))     state = "provisioning"
    }

    try {
        const attempt = $app.findRecordById("attempts", attemptId)
        attempt.set("current_state", state)
        $app.save(attempt)
        console.log(`[attempt:${attemptId}] current_state → ${state}`)
    } catch (err) {
        console.log(`[attempt:${attemptId}] error updating current_state: ${err}`)
    }

    if (state === "decommissioned") {
        try {
            const cfgs = $app.findRecordsByFilter("attempt_configs", "attempt = {:id}", "", 1, 0, { id: attemptId })
            if (cfgs.length > 0) {
                cfgs[0].set("finished", new Date().toISOString())
                $app.save(cfgs[0])
            }
        } catch (err) {
            console.log(`[attempt:${attemptId}] error setting attempt_configs.finished: ${err}`)
        }
    }

    e.next()
}, "assets")

// ─── Cron: set desired_state=decommissioned on attempts with expired assets ────
cronAdd("decommission-expired-assets", "* * * * *", () => {
    const expiredAssets = $app.findRecordsByFilter(
        "assets",
        "expires_at != '' && expires_at <= @now && state != 'decommissioning' && state != 'decommissioned'",
        "", 0, 0,
    )

    const attemptIds = new Set(expiredAssets.map(a => a.getString("attempt")))
    for (const id of attemptIds) {
        try {
            const attempt = $app.findRecordById("attempts", id)
            if (attempt.getString("desired_state") !== "decommissioned") {
                attempt.set("desired_state", "decommissioned")
                $app.save(attempt)
                console.log(`[cron] attempt ${id} → desired_state decommissioned (asset expired)`)
            }
        } catch (err) {
            console.log(`[cron] error setting desired_state for attempt ${id}: ${err}`)
        }
    }
})
