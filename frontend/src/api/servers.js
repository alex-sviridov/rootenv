import { pb } from '@/lib/pb'

export const fetchServers = (attemptId) =>
  pb.collection('assets_userview').getFullList({
    filter: `attempt_id = "${attemptId}"`,
  })

export const subscribeToServers = async (attemptId, callback) => {
  const refresh = (id) =>
    pb.collection('assets_userview')
      .getFirstListItem(`id = "${id}" && attempt_id = "${attemptId}"`, { requestKey: null })
      .then(record => callback({ action: 'update', record }))
      .catch(() => {})

  return pb.collection('assets').subscribe('*', (event) => {
    if (event.action === 'delete') {
      callback({ action: 'delete', record: { id: event.record.id } })
    } else {
      refresh(event.record.id)
    }
  })
}
