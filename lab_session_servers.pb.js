/// <reference path="../pb_data/types.d.ts" />



onRecordAfterCreateSuccess((e) => {

  const serverId = e.record.id
  const server = $app.findRecordById("lab_session_servers", serverId)
  const state = server.getString("provision_state")

  // pending >> provisioning
  if (state == "pending") {
    server.set("provision_state", "provisioning")
    server.set("vm_ip", "116.202.18.202")
    server.set("vm_ssh_port", "2")
    server.set("vm_ssh_user", "anisble")
    server.set("vm_ssh_key", "XXX")
    $app.save(server)
    e.next()
    return
  }

  e.next()
}, "lab_session_servers")

onRecordAfterUpdateSuccess((e) => {

  const serverId = e.record.id
  const server = $app.findRecordById("lab_session_servers", serverId)
  const state = server.getString("provision_state")

  // provisioning >> provisioned
  if (state == "provisioning") {
    server.set("provision_state", "provisioned")
    server.set("status", "available")
    $app.save(server)

    // Check if all servers for the same lab_session are now provisioned
    const sessionId = server.getString("lab_session")
    const allServers = $app.findRecordsByFilter(
      "lab_session_servers",
      "lab_session = {:sessionId}",
      "", 0, 0,
      {"sessionId": sessionId}
    )
    const allProvisioned = allServers.every(s => s.getString("provision_state") === "provisioned")
    if (allProvisioned) {
      const session = $app.findRecordById("lab_sessions", sessionId)
      session.set("status", "running")
      $app.save(session)
    }

    e.next()
    return
  }

  // destroying >> destroyed
  if (state == "destroying") {
    server.set("provision_state", "destroyed")
    server.set("status", "")
    $app.save(server)

    // Check if all servers for the same lab_session are now destroyed
    const sessionId = server.getString("lab_session")
    const allServers = $app.findRecordsByFilter(
      "lab_session_servers",
      "lab_session = {:sessionId}",
      "", 0, 0,
      {"sessionId": sessionId}
    )
    const allDestroyed = allServers.every(s => s.getString("provision_state") === "destroyed")
    if (allDestroyed) {
      const session = $app.findRecordById("lab_sessions", sessionId)
      session.set("status", "stopped")
      $app.save(session)
    }

    e.next()
    return
  }

  e.next()
}, "lab_session_servers")


