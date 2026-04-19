/// <reference path="../pb_data/types.d.ts" />
migrate((app) => {
  const collection = app.findCollectionByNameOrId("pbc_3143837116")

  // update collection data
  unmarshal({
    "viewQuery": "SELECT s.id, s.name, a.user, s.status, s.state, a.id as attempt_id\nFROM servers s, attempts a\nWHERE s.attempt = a.id AND s.state != \"decommissioned\"\n"
  }, collection)

  // remove field
  collection.fields.removeById("_clone_Edq7")

  // remove field
  collection.fields.removeById("_clone_2T7A")

  // remove field
  collection.fields.removeById("_clone_3fvi")

  // remove field
  collection.fields.removeById("_clone_uRvf")

  // add field
  collection.fields.addAt(1, new Field({
    "autogeneratePattern": "",
    "hidden": false,
    "id": "_clone_Rd3n",
    "max": 0,
    "min": 0,
    "name": "name",
    "pattern": "",
    "presentable": false,
    "primaryKey": false,
    "required": false,
    "system": false,
    "type": "text"
  }))

  // add field
  collection.fields.addAt(2, new Field({
    "cascadeDelete": false,
    "collectionId": "_pb_users_auth_",
    "hidden": false,
    "id": "_clone_zvnk",
    "maxSelect": 1,
    "minSelect": 0,
    "name": "user",
    "presentable": false,
    "required": true,
    "system": false,
    "type": "relation"
  }))

  // add field
  collection.fields.addAt(3, new Field({
    "hidden": false,
    "id": "_clone_F74X",
    "maxSelect": 1,
    "name": "status",
    "presentable": false,
    "required": false,
    "system": false,
    "type": "select",
    "values": [
      "poweredon",
      "rebooting",
      "poweredoff"
    ]
  }))

  // add field
  collection.fields.addAt(4, new Field({
    "hidden": false,
    "id": "_clone_Wv3p",
    "maxSelect": 1,
    "name": "state",
    "presentable": false,
    "required": false,
    "system": false,
    "type": "select",
    "values": [
      "pending",
      "provisioning",
      "provisioned",
      "decommissioning",
      "decommissioned"
    ]
  }))

  return app.save(collection)
}, (app) => {
  const collection = app.findCollectionByNameOrId("pbc_3143837116")

  // update collection data
  unmarshal({
    "viewQuery": "SELECT s.id, s.name, a.user, s.status, s.state, a.id as attempt_id\nFROM servers s, attempts a\nWHERE s.attempt = a.id \n"
  }, collection)

  // add field
  collection.fields.addAt(1, new Field({
    "autogeneratePattern": "",
    "hidden": false,
    "id": "_clone_Edq7",
    "max": 0,
    "min": 0,
    "name": "name",
    "pattern": "",
    "presentable": false,
    "primaryKey": false,
    "required": false,
    "system": false,
    "type": "text"
  }))

  // add field
  collection.fields.addAt(2, new Field({
    "cascadeDelete": false,
    "collectionId": "_pb_users_auth_",
    "hidden": false,
    "id": "_clone_2T7A",
    "maxSelect": 1,
    "minSelect": 0,
    "name": "user",
    "presentable": false,
    "required": true,
    "system": false,
    "type": "relation"
  }))

  // add field
  collection.fields.addAt(3, new Field({
    "hidden": false,
    "id": "_clone_3fvi",
    "maxSelect": 1,
    "name": "status",
    "presentable": false,
    "required": false,
    "system": false,
    "type": "select",
    "values": [
      "poweredon",
      "rebooting",
      "poweredoff"
    ]
  }))

  // add field
  collection.fields.addAt(4, new Field({
    "hidden": false,
    "id": "_clone_uRvf",
    "maxSelect": 1,
    "name": "state",
    "presentable": false,
    "required": false,
    "system": false,
    "type": "select",
    "values": [
      "pending",
      "provisioning",
      "provisioned",
      "decommissioning",
      "decommissioned"
    ]
  }))

  // remove field
  collection.fields.removeById("_clone_Rd3n")

  // remove field
  collection.fields.removeById("_clone_zvnk")

  // remove field
  collection.fields.removeById("_clone_F74X")

  // remove field
  collection.fields.removeById("_clone_Wv3p")

  return app.save(collection)
})
