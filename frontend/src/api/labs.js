import { pb } from '@/lib/pb'

const list = filter =>
  pb.collection('labs_userview').getFullList({ filter, sort: 'title', fields: 'id,title,description' })

export const fetchFolders = () => list('type = "folder" && parent = ""')
export const fetchLabsInFolder = id => list(`type = "lab" && parent = "${id}"`)
export const fetchLab = id => pb.collection('labs_userview').getOne(id)
