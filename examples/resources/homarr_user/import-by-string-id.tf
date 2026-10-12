import {
  to = homarr_user.viewer
  id = "hn8u8h18nvpxbvjryfdng4jt"
}

# Or by name. Only accounts that sign in with a Homarr password can be imported.
import {
  to = homarr_user.operator
  id = "username=operator"
}

# Leave group_ids out of imported users: memberships cannot be read back, so a
# configured group_ids would plan a replacement, deleting the account.
