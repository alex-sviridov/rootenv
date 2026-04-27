import { pb } from '@/lib/pb'

export const fetchLastAttempt = (labId) =>
  pb.collection('attempts_userview').getFirstListItem(`lab = "${labId}"`, { sort: '-updated', requestKey: `last-attempt-${labId}` })

export const fetchAttempts = (labId, page, perPage) =>
  pb.collection('attempts_userview').getList(page, perPage, {
    filter: `lab = "${labId}"`,
    sort: '-updated',
    requestKey: `attempts-list-${labId}-${page}`,
  })

export const createAttempt = (labId, labName) =>
  pb.collection('attempts').create({ lab: labId, lab_name: labName, user: pb.authStore.record.id })

export const fetchActiveAttempt = () =>
  pb.collection('attempts_userview')
    .getFirstListItem('state != "decommissioned"', { requestKey: 'active-attempt' })
    .catch(e => e?.status === 404 ? null : Promise.reject(e))

export const decommissionAttempt = (serverIds) =>
  Promise.all(serverIds.map(id =>
    pb.collection('commands').create({ asset: id, command: 'decommission', status: 'pending' }, { requestKey: id })
  ))

export const fetchAssetSecret = (assetId) =>
  pb.collection('keys_userview')
    .getFirstListItem(`asset = "${assetId}"`, { requestKey: `asset-secret-${assetId}` })
    .then(r => r.secret)
