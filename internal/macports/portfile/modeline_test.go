package portfile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// MacPorts' modeline is added where line 1 is none, replaces one with its
// settings spelled otherwise, and leaves one that sets others, as tabs, to
// the maintainer (the person's ask, 2026-10-24).
func TestTheModelineIsAddedOrCorrected(t *testing.T) {
	t.Parallel()
	body := "PortSystem 1.0\nname fixture\n"
	for _, c := range []struct {
		why, src, want string
		changed        bool
	}{
		{"as create writes it", Modeline + "\n\n" + body, Modeline + "\n\n" + body, false},
		{"none", body, Modeline + "\n\n" + body, true},
		{"none, a blank line first", "\n" + body, Modeline + "\n\n" + body, true},
		{"filetype spelled out", "# -*- coding: utf-8; mode: tcl; tab-width: 4; indent-tabs-mode: nil; c-basic-offset: 4 -*- vim:fenc=utf-8:filetype=tcl:et:sw=4:ts=4:sts=4\n\n" + body, Modeline + "\n\n" + body, true},
		{"no mode: tcl", "# -*- coding: utf-8; tab-width: 4; indent-tabs-mode: nil; c-basic-offset: 4 -*- vim:fenc=utf-8:ft=tcl:et:sw=4:ts=4:sts=4\n\n" + body, Modeline + "\n\n" + body, true},
		{"vim: with a space", "# -*- coding: utf-8; mode: tcl; tab-width: 4; indent-tabs-mode: nil; c-basic-offset: 4 -*- vim: fenc=utf-8:ft=tcl:et:sw=4:ts=4:sts=4\n\n" + body, Modeline + "\n\n" + body, true},
		{"tabs", "# -*- coding: utf-8; mode: tcl; tab-width: 8; indent-tabs-mode: t; c-basic-offset: 8 -*- vim:fenc=utf-8:ft=tcl:noet:sw=8:ts=8\n\n" + body, "# -*- coding: utf-8; mode: tcl; tab-width: 8; indent-tabs-mode: t; c-basic-offset: 8 -*- vim:fenc=utf-8:ft=tcl:noet:sw=8:ts=8\n\n" + body, false},
		{"another width", "# vim: set ts=2 sw=2 :\n" + body, "# vim: set ts=2 sw=2 :\n" + body, false},
	} {
		got, changed := WithModeline([]byte(c.src))
		require.Equal(t, c.want, string(got), c.why)
		require.Equal(t, c.changed, changed, c.why)
	}
}
