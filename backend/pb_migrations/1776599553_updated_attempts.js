/// <reference path="../pb_data/types.d.ts" />
migrate((app) => {
  const collection = app.findCollectionByNameOrId("pbc_4287217533")

  // add field
  collection.fields.addAt(5, new Field({
    "hidden": false,
    "id": "date2790239036",
    "max": "",
    "min": "",
    "name": "finished",
    "presentable": false,
    "required": false,
    "system": false,
    "type": "date"
  }))

  return app.save(collection)
}, (app) => {
  const collection = app.findCollectionByNameOrId("pbc_4287217533")

  // remove field
  collection.fields.removeById("date2790239036")

  return app.save(collection)
})
