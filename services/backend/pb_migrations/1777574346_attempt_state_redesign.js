/// <reference path="../pb_data/types.d.ts" />

migrate((app) => {
    // ── 1. Add current_state + desired_state to attempts ──────────────────────
    const attempts = app.findCollectionByNameOrId("attempts")

    const currentStateField = new Field({
        "id": "select_current_state",
        "name": "current_state",
        "type": "select",
        "maxSelect": 1,
        "required": false,
        "presentable": false,
        "system": false,
        "hidden": false,
        "values": ["new", "provisioning", "provisioned", "decommissioning", "decommissioned"]
    })
    attempts.fields.add(currentStateField)

    const desiredStateField = new Field({
        "id": "select_desired_state",
        "name": "desired_state",
        "type": "select",
        "maxSelect": 1,
        "required": false,
        "presentable": false,
        "system": false,
        "hidden": false,
        "values": ["provisioned", "decommissioned"]
    })
    attempts.fields.add(desiredStateField)

    // Update access rules: users can list/view directly; can only patch desired_state
    attempts.listRule = "@request.auth.id = user.id || @request.auth.svc_role = 'relay' || @request.auth.svc_role = 'contmgr'"
    attempts.viewRule = "@request.auth.id = user.id || @request.auth.svc_role = 'relay' || @request.auth.svc_role = 'contmgr'"
    attempts.updateRule = "@request.auth.id = user.id"
    attempts.createRule = "@request.auth.id = user.id"

    app.save(attempts)

    // ── 2. Backfill current_state + desired_state on existing attempts ─────────
    const allAttempts = app.findRecordsByFilter("attempts", "1=1", "", 0, 0)
    for (const a of allAttempts) {
        const finished = a.getString("finished")
        if (finished && finished > "2000-01-01") {
            a.set("current_state", "decommissioned")
            a.set("desired_state", "decommissioned")
        } else {
            a.set("current_state", "new")
            a.set("desired_state", "provisioned")
        }
        app.save(a)
    }

    // ── 3. Create attempt_configs collection ───────────────────────────────────
    const attemptConfigs = new Collection({
        "id": "pbc_attempt_configs",
        "name": "attempt_configs",
        "type": "base",
        "system": false,
        "createRule": null,
        "listRule": "@request.auth.svc_role = 'relay' || @request.auth.svc_role = 'contmgr'",
        "viewRule": "@request.auth.svc_role = 'relay' || @request.auth.svc_role = 'contmgr'",
        "updateRule": "@request.auth.svc_role = 'relay' || @request.auth.svc_role = 'contmgr'",
        "deleteRule": null,
        "fields": [
            {
                "id": "text3208210256",
                "name": "id",
                "type": "text",
                "autogeneratePattern": "[a-z0-9]{15}",
                "max": 15,
                "min": 15,
                "pattern": "^[a-z0-9]+$",
                "required": true,
                "primaryKey": true,
                "system": true,
                "hidden": false,
                "presentable": false
            },
            {
                "id": "relation_ac_attempt",
                "name": "attempt",
                "type": "relation",
                "collectionId": "pbc_4287217533",
                "cascadeDelete": true,
                "maxSelect": 1,
                "minSelect": 0,
                "required": true,
                "system": false,
                "hidden": false,
                "presentable": false
            },
            {
                "id": "json_ac_environment",
                "name": "environment",
                "type": "json",
                "maxSize": 0,
                "required": false,
                "system": false,
                "hidden": false,
                "presentable": false
            },
            {
                "id": "date_ac_finished",
                "name": "finished",
                "type": "date",
                "max": "",
                "min": "",
                "required": false,
                "system": false,
                "hidden": false,
                "presentable": false
            },
            {
                "id": "autodate2990389176",
                "name": "created",
                "type": "autodate",
                "onCreate": true,
                "onUpdate": false,
                "system": false,
                "hidden": false,
                "presentable": false
            },
            {
                "id": "autodate3332085495",
                "name": "updated",
                "type": "autodate",
                "onCreate": true,
                "onUpdate": true,
                "system": false,
                "hidden": false,
                "presentable": false
            }
        ]
    })
    app.save(attemptConfigs)

    // ── 4. Backfill attempt_configs from existing attempts ─────────────────────
    const attemptConfigsCol = app.findCollectionByNameOrId("attempt_configs")
    const attemptsForBackfill = app.findRecordsByFilter("attempts", "1=1", "", 0, 0)
    for (const a of attemptsForBackfill) {
        const cfg = new Record(attemptConfigsCol)
        cfg.set("attempt", a.id)
        cfg.set("environment", a.get("environment"))
        const finished = a.getString("finished")
        if (finished && finished > "2000-01-01") {
            cfg.set("finished", finished)
        }
        app.save(cfg)
    }

    // ── 5. Drop attempts_userview ──────────────────────────────────────────────
    const attemptsUserview = app.findCollectionByNameOrId("attempts_userview")
    app.delete(attemptsUserview)

    // ── 6. Update keys_userview: filter by current_state instead of finished ───
    const keysUserview = app.findCollectionByNameOrId("keys_userview")
    keysUserview.viewQuery = "SELECT k.id, k.asset, ast.attempt, a.user, k.secret FROM keys k JOIN assets ast ON k.asset = ast.id JOIN attempts a ON ast.attempt = a.id WHERE a.current_state != 'decommissioned'"
    app.save(keysUserview)

    // ── 7. Remove environment + finished fields from attempts ──────────────────
    const attemptsUpdated = app.findCollectionByNameOrId("attempts")
    attemptsUpdated.fields.removeById("json1176952354")  // environment
    attemptsUpdated.fields.removeById("date2790239036")  // finished
    app.save(attemptsUpdated)

}, (app) => {
    // ── Down: restore attempts fields and rules ────────────────────────────────
    const attempts = app.findCollectionByNameOrId("attempts")

    attempts.fields.add(new Field({
        "id": "json1176952354",
        "name": "environment",
        "type": "json",
        "maxSize": 0,
        "required": false,
        "system": false,
        "hidden": false,
        "presentable": false
    }))
    attempts.fields.add(new Field({
        "id": "date2790239036",
        "name": "finished",
        "type": "date",
        "max": "",
        "min": "",
        "required": false,
        "system": false,
        "hidden": false,
        "presentable": false
    }))
    attempts.fields.removeById("select_current_state")
    attempts.fields.removeById("select_desired_state")
    attempts.listRule = "  @request.auth.svc_role = \"relay\""
    attempts.viewRule = "  @request.auth.svc_role = \"relay\" ||   @request.auth.svc_role = \"contmgr\""
    attempts.updateRule = null
    attempts.createRule = "@request.auth.id = user.id"
    app.save(attempts)

    // Restore keys_userview
    const keysUserview = app.findCollectionByNameOrId("keys_userview")
    keysUserview.viewQuery = "SELECT k.id, k.asset, ast.attempt, a.user, k.secret FROM keys k JOIN assets ast ON k.asset = ast.id JOIN attempts a ON ast.attempt = a.id WHERE a.finished IS NULL OR a.finished = ''"
    app.save(keysUserview)

    // Drop attempt_configs
    try {
        const attemptConfigs = app.findCollectionByNameOrId("attempt_configs")
        app.delete(attemptConfigs)
    } catch (_) {}

    // Recreate attempts_userview
    const attemptsUserview = new Collection({
        "id": "pbc_3760174492",
        "name": "attempts_userview",
        "type": "view",
        "system": false,
        "createRule": null,
        "listRule": "@request.auth.id = user",
        "viewRule": "@request.auth.id = user",
        "updateRule": null,
        "deleteRule": null,
        "viewQuery": "SELECT id, created, updated, finished, user, lab_name, lab, state FROM (\n    SELECT\n        a.id,\n        a.created,\n        a.updated,\n        a.finished,\n        CAST(a.user AS TEXT) AS user,\n        CAST(a.lab_name AS TEXT) AS lab_name,\n        CAST(a.lab AS TEXT) AS lab,\n        CASE\n            WHEN finished > '01/01/2000' THEN 'decommissioned'\n            WHEN COUNT(s.id) = 0                                                        THEN 'new'\n            WHEN SUM(CASE WHEN s.state = 'decommissioning' THEN 1 ELSE 0 END) > 0      THEN 'decommissioning'\n            WHEN SUM(CASE WHEN s.state != 'decommissioned' THEN 1 ELSE 0 END) = 0      THEN 'decommissioned'\n            WHEN SUM(CASE WHEN s.state = 'provisioning'    THEN 1 ELSE 0 END) > 0      THEN 'provisioning'\n            WHEN SUM(CASE WHEN s.state = 'pending'    THEN 1 ELSE 0 END) > 0      THEN 'provisioning'\n            WHEN SUM(CASE WHEN s.state != 'provisioned'    THEN 1 ELSE 0 END) = 0      THEN 'provisioned'\n            ELSE 'new'\n        END AS state\n    FROM attempts a\n    LEFT JOIN assets s ON s.attempt = a.id\n    GROUP BY a.id, a.user, a.lab_name, a.lab\n)"
    })
    app.save(attemptsUserview)
})
