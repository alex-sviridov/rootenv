/// <reference path="../pb_data/types.d.ts" />
migrate((app) => {
  const collection = app.findCollectionByNameOrId("pbc_3760174492")

  // update collection data
  unmarshal({
    "viewQuery": "SELECT id, updated, user, lab_name, state FROM (\n    SELECT\n        a.id,\n        a.updated,\n        CAST(a.user AS TEXT) AS user,\n        CAST(a.lab_name AS TEXT) AS lab_name,\n        CASE\n            WHEN COUNT(s.id) = 0                                                        THEN 'new'\n            WHEN SUM(CASE WHEN s.state = 'decommissioning' THEN 1 ELSE 0 END) > 0      THEN 'decommissioning'\n            WHEN SUM(CASE WHEN s.state != 'decommissioned' THEN 1 ELSE 0 END) = 0      THEN 'decommissioned'\n            WHEN SUM(CASE WHEN s.state = 'provisioning'    THEN 1 ELSE 0 END) > 0      THEN 'provisioning'\n            WHEN SUM(CASE WHEN s.state = 'pending'    THEN 1 ELSE 0 END) > 0      THEN 'provisioning'\n            WHEN SUM(CASE WHEN s.state != 'provisioned'    THEN 1 ELSE 0 END) = 0      THEN 'provisioned'\n            ELSE 'new'\n        END AS state\n    FROM attempts a\n    LEFT JOIN servers s ON s.attempt = a.id\n    GROUP BY a.id, a.user, a.lab_name\n)"
  }, collection)

  return app.save(collection)
}, (app) => {
  const collection = app.findCollectionByNameOrId("pbc_3760174492")

  // update collection data
  unmarshal({
    "viewQuery": "SELECT id, updated, user, lab_name, state FROM (\n    SELECT\n        a.id,\n        a.updated,\n        CAST(a.user AS TEXT) AS user,\n        CAST(a.lab_name AS TEXT) AS lab_name,\n        CASE\n            WHEN COUNT(s.id) = 0                                                        THEN 'new'\n            WHEN SUM(CASE WHEN s.state = 'decommissioning' THEN 1 ELSE 0 END) > 0      THEN 'decommissioning'\n            WHEN SUM(CASE WHEN s.state != 'decommissioned' THEN 1 ELSE 0 END) = 0      THEN 'decommissioned'\n            WHEN SUM(CASE WHEN s.state = 'provisioning'    THEN 1 ELSE 0 END) > 0      THEN 'provisioning'\n            WHEN SUM(CASE WHEN s.state != 'provisioned'    THEN 1 ELSE 0 END) = 0      THEN 'provisioned'\n            ELSE 'new'\n        END AS state\n    FROM attempts a\n    LEFT JOIN servers s ON s.attempt = a.id\n    GROUP BY a.id, a.user, a.lab_name\n)"
  }, collection)

  return app.save(collection)
})
