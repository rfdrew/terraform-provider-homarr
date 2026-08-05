# Boards are imported as "<board_id>,<column_count>". The column count has to be
# supplied by hand because Homarr's REST API does not report it; read it from the
# board's layout settings in the UI (the UI default is 10).
#
# Importing with a bare id is rejected on purpose: column_count would land in
# state as null, and because a change to it forces replacement, the next plan
# would propose destroying the board you just imported.
terraform import homarr_board.infra csgjeb9j3y2mflvznts1ip0w,12
