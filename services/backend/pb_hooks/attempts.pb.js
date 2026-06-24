// ─── Before update: block regular users from writing current_state ────────────
onRecordUpdateRequest((e) => {
    const auth = e.auth
    // No auth = admin/hook context ($app.save); allow unconditionally.
    if (!auth) { e.next(); return }
    // Service accounts (contmgr, relay) may write any field via API.
    const svcRole = auth.getString ? auth.getString("svc_role") : ""
    if (svcRole === "attempt-controller" ) { e.next(); return }
    if (e.requestInfo().body["expires_at"] !== undefined) {
        throw new ForbiddenError("expires_at is read-only")
    }
    if (e.requestInfo().body["assets"] !== undefined) {
        throw new ForbiddenError("expires_at is read-only")
    }
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