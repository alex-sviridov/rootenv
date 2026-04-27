import { pb } from '@/lib/pb'

export const fetchServers = (attemptId) =>
  pb.collection('assets').getFullList({
    filter: `attempt = "${attemptId}"`,
    requestKey: `servers-${attemptId}`,
  })

export const subscribeToServers = async (attemptId, callback) =>
  pb.collection('assets').subscribe('*', (event) => {
    if (event.record.attempt !== attemptId) return
    if (event.action === 'delete') {
      callback({ action: 'delete', record: { id: event.record.id } })
    } else {
      callback({ action: 'update', record: event.record })
    }
  })
