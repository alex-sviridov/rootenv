/// <reference path="../pb_data/types.d.ts" />
migrate((app) => {
  const collection = app.findCollectionByNameOrId("pbc_3738798621")

  // update field
  collection.fields.addAt(8, new Field({
    "hidden": false,
    "id": "date617435213",
    "max": "",
    "min": "",
    "name": "expires_at",
    "presentable": false,
    "required": false,
    "system": false,
    "type": "date"
  }))

  return app.save(collection)
}, (app) => {
  const collection = app.findCollectionByNameOrId("pbc_3738798621")

  // update field
  collection.fields.addAt(8, new Field({
    "hidden": false,
    "id": "date617435213",
    "max": "",
    "min": "",
    "name": "expiration",
    "presentable": false,
    "required": false,
    "system": false,
    "type": "date"
  }))

  return app.save(collection)
})
