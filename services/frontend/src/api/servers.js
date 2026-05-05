import { pb } from '@/lib/pb'

function parseServerRecord(record) {
  const raw = record.protocols
  const protocols = Array.isArray(raw)
    ? raw
    : typeof raw === 'string' ? (() => { try { return JSON.parse(raw) } catch { return [] } })()
    : []
  return { ...record, protocols }
}

export const fetchServers = (attemptId) =>
  pb.collection('assets').getFullList({
    filter: `attempt = "${attemptId}"`,
    requestKey: `servers-${attemptId}`,
  }).then(records => records.map(parseServerRecord))

export const subscribeToServers = async (attemptId, callback) =>
  pb.collection('assets').subscribe('*', (event) => {
    if (event.record.attempt !== attemptId) return
    if (event.action === 'delete') {
      callback({ action: 'delete', record: { id: event.record.id } })
    } else {
      callback({ action: 'update', record: parseServerRecord(event.record) })
    }
  })
