/// <reference path="../pb_data/types.d.ts" />
migrate((app) => {
  const collection = app.findCollectionByNameOrId("pbc_3664792373")

  // update collection data
  unmarshal({
    "createRule": "@request.auth.id = server.attempt.user.id"
  }, collection)

  return app.save(collection)
}, (app) => {
  const collection = app.findCollectionByNameOrId("pbc_3664792373")

  // update collection data
  unmarshal({
    "createRule": null
  }, collection)

  return app.save(collection)
})
