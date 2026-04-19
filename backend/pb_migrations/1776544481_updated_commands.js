/// <reference path="../pb_data/types.d.ts" />
migrate((app) => {
  const collection = app.findCollectionByNameOrId("pbc_3664792373")

  // update field
  collection.fields.addAt(2, new Field({
    "hidden": false,
    "id": "select2395663060",
    "maxSelect": 1,
    "name": "command",
    "presentable": false,
    "required": false,
    "system": false,
    "type": "select",
    "values": [
      "start",
      "stop",
      "restart",
      "decommission"
    ]
  }))

  return app.save(collection)
}, (app) => {
  const collection = app.findCollectionByNameOrId("pbc_3664792373")

  // update field
  collection.fields.addAt(2, new Field({
    "hidden": false,
    "id": "select2395663060",
    "maxSelect": 1,
    "name": "command",
    "presentable": false,
    "required": false,
    "system": false,
    "type": "select",
    "values": [
      "start",
      "stop",
      "restart"
    ]
  }))

  return app.save(collection)
})
