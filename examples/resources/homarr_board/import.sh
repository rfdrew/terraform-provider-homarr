# Boards are imported as "<board_id>,<column_count>". The column count has to be
# supplied by hand because Homarr's REST API does not report it; read it from the
# board's layout settings in the UI (the UI default is 10).
#
# Importing with a bare id is rejected on purpose: column_count would land in
# state as null, and because a change to it forces replacement, the next plan
# would propose destroying the board you just imported.
terraform import homarr_board.infra csgjeb9j3y2mflvznts1ip0w,12

# The board can also be named instead of identified, as "name=<name>,<column_count>".
# The name is matched exactly first, then case-insensitively.
terraform import homarr_board.infra name=infra,12

# base_layout_id stays null after an import: Homarr returns a board's layout id
# only in the response that created it.
