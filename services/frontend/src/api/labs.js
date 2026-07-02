import { pb } from '@/lib/pb'

const listFields = 'id,title,description,type,slug,group_id,group_title'

const list = filter =>
  pb.collection('labs_userview').getFullList({ filter, sort: 'title', fields: listFields })

export const fetchFolders = () => list('type = "folder" && group_id = ""')
export const fetchLabsInFolder = id => list(`type = "lab" && group_id = "${id}"`)
export const fetchLab = id => pb.collection('labs_userview').getOne(id)
