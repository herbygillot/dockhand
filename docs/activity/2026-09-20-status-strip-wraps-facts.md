# The status strip wraps a message at its facts

A refresh that retires a merged contribution reports everything it did as one message: the PR's state, the retirement, and the local and fork branches it deleted, joined by "; ". The status screen's message strip wrote each message on one line and cut it at the terminal's edge, so the branch names, the part most worth reading, were the part lost.

The strip now packs a message's facts onto a line while they fit the width and continues on indented lines when they do not, at render time, since the width is only known there. A single fact wider than the screen is still truncated, which is the only case left where the ellipsis appears. The strip keeps its line budget: it shows its newest lines within the number of lines the layout reserves, so a long message costs older ones their place rather than the table its rows. A short message is one line, as before.
