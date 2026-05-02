# frontend
- allow moving border between labcontent and labconsole, hovering on border show buttons to hide (not destroying component just hide from user) one or another. 
- hide console when lab is not started
- allow minimizing sidebar keeping minimal info
- tabs reordering make better, more contrasted, color tabs where sever is unavailable
- add attempts list
- close tabs on decommission or do something like that`
- false 'Another lab is active' message in the same lab

# relay
- http forwarding into iframe
- filemanager

# contmgr

- make cleaner user creation

- allow post-provision scripts

- more clear definition of container registry used

# backend

- add svc users creation to migration




fix errors
[contmgr] 2026/05/02 13:30:25 ERROR provision asset asset=k3j1hc297fsh91l err="write authorized_keys: unable to upgrade connection: container not found (\"workstation\")"
[contmgr] 2026/05/02 13:30:16 ERROR provision asset asset=6z3lqodfaisrgbu err="invalid asset def: asset def: ssh_user required"

- Restrict how many Pods you can create within a namespace.