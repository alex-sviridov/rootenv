import { pb } from '@/lib/pb'

export const fetchLastAttempt = (labId) =>
  pb.collection('attempts').getFirstListItem(`lab = "${labId}"`, { sort: '-created', requestKey: `last-attempt-${labId}` })

export const fetchAttempts = (labId, page, perPage) =>
  pb.collection('attempts').getList(page, perPage, {
    filter: `lab = "${labId}"`,
    sort: '-updated',
    requestKey: `attempts-list-${labId}-${page}`,
  })

export const createAttempt = (labId, labName) =>
  pb.collection('attempts').create({ lab: labId, lab_name: labName, user: pb.authStore.record.id })

export const fetchActiveAttempt = () =>
  pb.collection('attempts')
    .getFirstListItem('current_state != "decommissioned"', { requestKey: 'active-attempt' })
    .catch(e => e?.status === 404 ? null : Promise.reject(e))

export const decommissionAttempt = (attemptId) =>
  pb.collection('attempts').update(attemptId, { desired_state: 'decommissioned' })

export const subscribeToAttempt = (attemptId, callback) =>
  pb.collection('attempts').subscribe(attemptId, callback)
