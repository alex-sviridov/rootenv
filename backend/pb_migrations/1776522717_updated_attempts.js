/// <reference path="../pb_data/types.d.ts" />
migrate((app) => {
  const collection = app.findCollectionByNameOrId("pbc_4287217533")

  // add field
  collection.fields.addAt(4, new Field({
    "hidden": false,
    "id": "json1176952354",
    "maxSize": 0,
    "name": "environment",
    "presentable": false,
    "required": false,
    "system": false,
    "type": "json"
  }))

  return app.save(collection)
}, (app) => {
  const collection = app.findCollectionByNameOrId("pbc_4287217533")

  // remove field
  collection.fields.removeById("json1176952354")

  return app.save(collection)
})
