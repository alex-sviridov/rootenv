/// <reference path="../pb_data/types.d.ts" />
migrate((app) => {
  const collection = app.findCollectionByNameOrId("pbc_3760174492")

  // update collection data
  unmarshal({
    "viewQuery": "SELECT id, user, lab_name, state FROM (\n    SELECT\n        a.id,\n        a.user,\n        a.lab_name,\n        CASE\n            WHEN COUNT(s.id) = 0                                                        THEN 'new'\n            WHEN SUM(CASE WHEN s.state = 'decommissioning' THEN 1 ELSE 0 END) > 0      THEN 'decommissioning'\n            WHEN SUM(CASE WHEN s.state != 'decommissioned' THEN 1 ELSE 0 END) = 0      THEN 'decommissioned'\n            WHEN SUM(CASE WHEN s.state = 'provisioning'    THEN 1 ELSE 0 END) > 0      THEN 'provisioning'\n            WHEN SUM(CASE WHEN s.state != 'provisioned'    THEN 1 ELSE 0 END) = 0      THEN 'provisioned'\n            ELSE 'new'\n        END AS state\n    FROM attempts a\n    LEFT JOIN servers s ON s.attempt = a.id\n    GROUP BY a.id, a.user, a.lab_name\n)\n"
  }, collection)

  // remove field
  collection.fields.removeById("json1641460164")

  return app.save(collection)
}, (app) => {
  const collection = app.findCollectionByNameOrId("pbc_3760174492")

  // update collection data
  unmarshal({
    "viewQuery": "SELECT id, user, lab, lab_name, state FROM (\n    SELECT\n        a.id,\n        a.user,\n        a.lab,\n        a.lab_name,\n        CASE\n            WHEN COUNT(s.id) = 0                                                        THEN 'new'\n            WHEN SUM(CASE WHEN s.state = 'decommissioning' THEN 1 ELSE 0 END) > 0      THEN 'decommissioning'\n            WHEN SUM(CASE WHEN s.state != 'decommissioned' THEN 1 ELSE 0 END) = 0      THEN 'decommissioned'\n            WHEN SUM(CASE WHEN s.state = 'provisioning'    THEN 1 ELSE 0 END) > 0      THEN 'provisioning'\n            WHEN SUM(CASE WHEN s.state != 'provisioned'    THEN 1 ELSE 0 END) = 0      THEN 'provisioned'\n            ELSE 'new'\n        END AS state\n    FROM attempts a\n    LEFT JOIN servers s ON s.attempt = a.id\n    GROUP BY a.id, a.user, a.lab, a.lab_name\n)"
  }, collection)

  // add field
  collection.fields.addAt(2, new Field({
    "hidden": false,
    "id": "json1641460164",
    "maxSize": 1,
    "name": "lab",
    "presentable": false,
    "required": false,
    "system": false,
    "type": "json"
  }))

  return app.save(collection)
})
