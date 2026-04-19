import { pb } from '@/lib/pb'

export const fetchLastAttempt = (labId) =>
  pb.collection('attempts_userview').getFirstListItem(`lab = "${labId}"`, { sort: '-updated' })

export const fetchAttempts = (labId, page, perPage) =>
  pb.collection('attempts_userview').getList(page, perPage, {
    filter: `lab = "${labId}"`,
    sort: '-updated',
  })

export const createAttempt = (labId, labName) =>
  pb.collection('attempts').create({ lab: labId, lab_name: labName, user: pb.authStore.record.id })

export const subscribeToAttempt = async (labId, callback) => {
  const refresh = () =>
    pb.collection('attempts_userview')
      .getFirstListItem(`lab = "${labId}"`, { sort: '-updated', requestKey: null })
      .then(record => { if (record) callback(record) })
      .catch(() => {})

  const attemptUnsub = await pb.collection('attempts').subscribe('*', (event) => {
    if (event.record.lab === labId) refresh()
  })

  const serverUnsub = await pb.collection('servers').subscribe('*', refresh)

  return async () => { await attemptUnsub(); await serverUnsub() }
}

export const fetchActiveAttempt = () =>
  pb.collection('attempts_userview')
    .getFirstListItem('state != "decommissioned"')
    .catch(e => e?.status === 404 ? null : Promise.reject(e))

export const decommissionAttempt = (serverIds) =>
  Promise.all(serverIds.map(id =>
    pb.collection('commands').create({ server: id, command: 'decommission', status: 'pending' }, { requestKey: id })
  ))
