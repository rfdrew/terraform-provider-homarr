# A copy is imported by its board id, or by name as "name=<name>". Homarr keeps
# no record of which board a copy came from, so source_board_id is taken from
# your configuration by the first apply after the import, in place.
terraform import homarr_board_copy.team_a vbx4ghr2d8owm0c1t5k3zqje
terraform import homarr_board_copy.team_a name=team-a
