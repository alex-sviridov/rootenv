/// <reference path="../pb_data/types.d.ts" />

// ─── Cron: stop orphaned sessions stuck in 'stopping' with no active servers ──

cronAdd("close-empty-stopping-sessions", "* * * * *", () => {
  const stoppingSessions = $app.findRecordsByFilter(
    "lab_sessions",
    "status = 'stopping'",
    "", 0, 0, {}
  )

  for (const session of stoppingSessions) {
    const sessionId = session.id
    const activeServers = $app.findRecordsByFilter(
      "lab_session_servers",
      "lab_session = {:sessionId} && provision_state != 'destroyed'",
      "", 0, 0,
      { sessionId }
    )

    if (activeServers.length === 0) {
      session.set("status", "stopped")
      $app.save(session)
      console.log(`[cron] session ${sessionId} had no active servers, → stopped`)
    }
  }
})


// State machine (client-initiated transitions only):
//   starting → stopping  (force stop before provisioning completes)
//   running  → stopping  (normal stop)
//
// Server-managed transitions:
//   starting → running   (after provisioning)
//   stopping → stopped   (after teardown)

// ─── Create: validate and set server-side fields ──────────────────────────────

onRecordCreateRequest((e) => {
  const auth = e.auth
  if (!auth) throw new UnauthorizedError("authentication required")

  e.record.set("user", auth.id)
  e.record.set("status", "starting")
  e.record.set("started_at", new Date().toISOString())

  const active = $app.findRecordsByFilter(
    "lab_sessions",
    "user = {:userId} && status != 'stopped'",
    "", 1, 0,
    { userId: auth.id }
  )
  if (active.length > 0) {
    throw new BadRequestError("active session already exists: " + active[0].getString("lab"))
  }

  console.log(`[session:new] user=${auth.id} lab=${e.record.getString("lab")} → starting`)
  e.next()
}, "lab_sessions")


// ─── After create: provision — starting → running ─────────────────────────────

onRecordAfterCreateSuccess((e) => {
  const sessionId = e.record.id
  const labId = e.record.getString("lab")

  if (!labId) {
    console.log(`[session:${sessionId}] no lab set, skipping server creation`)
    e.next()
    return
  }

  let lab
  try {
    lab = $app.findRecordById("labs", labId)
  } catch {
    console.log(`[session:${sessionId}] lab '${labId}' not found, no servers created`)
    e.next()
    return
  }

  const serverDefs = lab.get("config") || []
  const serverCollection = $app.findCollectionByNameOrId("lab_session_servers")
  for (const def of serverDefs) {
    const server = new Record(serverCollection)
    server.set("lab_session", sessionId)
    server.set("name", def.name)
    server.set("provision_state", "pending")
    server.set("vm_ip", "")
    server.set("vm_ssh_port", "")
    server.set("vm_ssh_user", "")
    server.set("vm_ssh_key", "")
    $app.save(server)
    console.log(`[session:${sessionId}] created server '${def.name}'`)
  }
  e.next()
}, "lab_sessions")


// ─── Update: enforce valid transitions, owner or superuser only ───────────────

onRecordUpdateRequest((e) => {
  const auth = e.auth
  if (!auth) throw new UnauthorizedError("authentication required")

  const current = $app.findRecordById("lab_sessions", e.record.id)
  const isSuperuser = auth.isSuperuser()

  if (!isSuperuser && current.getString("user") !== auth.id) {
    throw new ForbiddenError("not your session")
  }

  const currentStatus = current.getString("status")
  const requestedStatus = e.requestInfo().body["status"]

  if (requestedStatus !== "stopping") {
    throw new BadRequestError("only 'stopping' is a valid client-initiated status update")
  }
  if (currentStatus !== "starting" && currentStatus !== "running") {
    throw new BadRequestError(`cannot stop session in '${currentStatus}' state`)
  }

  console.log(`[session:${e.record.id}] ${currentStatus} → stopping (by=${auth.id} superuser=${isSuperuser})`)
  e.record.set("status", "stopping")
  e.next()
}, "lab_sessions")


// ─── After update: teardown — stopping → stopped ──────────────────────────────

onRecordAfterUpdateSuccess((e) => {
  if (e.record.getString("status") !== "stopping") {
    e.next()
    return
  }

  const sessionId = e.record.id

  // Move all non-destroyed servers to destroying
  const servers = $app.findRecordsByFilter(
    "lab_session_servers",
    "lab_session = {:sessionId} && provision_state != 'destroyed'",
    "", 0, 0,
    { sessionId }
  )

  if (servers.length === 0) {
    // No servers to destroy — move session to stopped immediately
    e.record.set("status", "stopped")
    $app.save(e.record)
    console.log(`[session:${sessionId}] no servers, → stopped immediately`)
    e.next()
    return
  }

  for (const server of servers) {
    server.set("provision_state", "destroying")
    $app.save(server)
    console.log(`[session:${sessionId}] server '${server.getString("name")}' → destroying`)
  }

  e.next()
}, "lab_sessions")
