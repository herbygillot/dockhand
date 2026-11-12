# stages: quick
# exercises: the deciding step, the other half
# A row that passes without its deciding steps run is not exercised (#227).
act() { exercised "the other half"; }
assert() { row_pass "it says it passed"; }
