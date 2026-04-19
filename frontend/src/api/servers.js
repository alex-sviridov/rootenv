import { pb } from '@/lib/pb'

export const fetchServers = (attemptId) =>
  pb.collection('servers_userview').getFullList({
    filter: `attempt_id = "${attemptId}"`,
  })

const fetchServer = (id) =>
  pb.collection('servers_userview').getOne(id)

export const subscribeToServers = (attemptId, callback) =>
  pb.collection('servers').subscribe('*', async (event) => {
    if (event.record.attempt !== attemptId) return
    if (event.action === 'delete') {
      callback({ action: 'delete', record: { id: event.record.id } })
      return
    }
    const fresh = await fetchServer(event.record.id).catch(() => null)
    if (fresh) callback({ action: event.action, record: fresh })
  })

export const unsubscribeFromServers = () =>
  pb.collection('servers').unsubscribe('*')
