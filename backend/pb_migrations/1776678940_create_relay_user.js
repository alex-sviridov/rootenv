migrate((app) => {
  const collection = app.findCollectionByNameOrId("users")

  const record = new Record(collection)
  record.set("username", "svc_relay")
  record.set("email", "svc_relay@relay.local")
  record.set("password", "Secret123456")
  record.set("verified", true)
  record.set("svc_role", "relay")

  return app.save(record)
}, (app) => {
  const record = app.findAuthRecordByEmail("users", "svc_relay@relay.local")

  return app.deleteRecord(record)
})