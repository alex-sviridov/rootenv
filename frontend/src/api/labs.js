import PocketBase from 'pocketbase'

const pb = new PocketBase(import.meta.env.VITE_API_URL)

export async function fetchFolders() {
  const records = await pb.collection('labs_userview').getFullList({
    filter: 'type = "folder" && parent = ""',
    sort: 'title',
    fields: 'id,title,description',
  })
  return records
}

export async function fetchLabsInFolder(folderId) {
  const records = await pb.collection('labs_userview').getFullList({
    filter: `type = "lab" && parent = "${folderId}"`,
    sort: 'title',
    fields: 'id,title,description',
  })
  return records
}
