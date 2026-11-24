package portfile

import (
	"strings"
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
		{"spaced vim form", "# -*- coding: utf-8; mode: tcl; tab-width: 4; indent-tabs-mode: nil; c-basic-offset: 4 -*- vim: fenc=utf-8:ft=tcl:et:sw=4:ts=4:sts=4\n\n" + body, "# -*- coding: utf-8; mode: tcl; tab-width: 4; indent-tabs-mode: nil; c-basic-offset: 4 -*- vim: fenc=utf-8:ft=tcl:et:sw=4:ts=4:sts=4\n\n" + body, false},
		{"reordered options", "# -*- c-basic-offset: 4; indent-tabs-mode: nil; tab-width: 4; mode: tcl; coding: utf-8 -*- vim: sts=4:ts=4:sw=4:et:ft=tcl:fenc=utf-8\n\n" + body, "# -*- c-basic-offset: 4; indent-tabs-mode: nil; tab-width: 4; mode: tcl; coding: utf-8 -*- vim: sts=4:ts=4:sw=4:et:ft=tcl:fenc=utf-8\n\n" + body, false},
		{"long option names", "# -*- coding: utf-8; mode: tcl; tab-width: 4; indent-tabs-mode: nil; c-basic-offset: 4 -*- vim: fileencoding=utf-8:filetype=tcl:expandtab:shiftwidth=4:tabstop=4:softtabstop=4\n\n" + body, "# -*- coding: utf-8; mode: tcl; tab-width: 4; indent-tabs-mode: nil; c-basic-offset: 4 -*- vim: fileencoding=utf-8:filetype=tcl:expandtab:shiftwidth=4:tabstop=4:softtabstop=4\n\n" + body, false},
		{"vim set form", "# -*- coding: utf-8; mode: tcl; tab-width: 4; indent-tabs-mode: nil; c-basic-offset: 4 -*- vim: set fenc=utf-8 ft=tcl et sw=4 ts=4 sts=4 :\n\n" + body, "# -*- coding: utf-8; mode: tcl; tab-width: 4; indent-tabs-mode: nil; c-basic-offset: 4 -*- vim: set fenc=utf-8 ft=tcl et sw=4 ts=4 sts=4 :\n\n" + body, false},
		{"missing option", "# -*- coding: utf-8; mode: tcl; tab-width: 4; indent-tabs-mode: nil; c-basic-offset: 4 -*- vim: fenc=utf-8:ft=tcl:et:sw=4:ts=4\n\n" + body, Modeline + "\n\n" + body, true},
		{"additional option", "# -*- coding: utf-8; mode: tcl; tab-width: 4; indent-tabs-mode: nil; c-basic-offset: 4 -*- vim: fenc=utf-8:ft=tcl:et:sw=4:ts=4:sts=4:customoption=1\n\n" + body, Modeline + "\n\n" + body, true},
		{"another encoding", "# -*- coding: latin-1; mode: tcl; tab-width: 4; indent-tabs-mode: nil; c-basic-offset: 4 -*- vim:fenc=latin1:ft=tcl:et:sw=4:ts=4:sts=4\n\n" + body, "# -*- coding: latin-1; mode: tcl; tab-width: 4; indent-tabs-mode: nil; c-basic-offset: 4 -*- vim:fenc=latin1:ft=tcl:et:sw=4:ts=4:sts=4\n\n" + body, false},
		{"another mode", "# -*- coding: utf-8; mode: python; tab-width: 4; indent-tabs-mode: nil; c-basic-offset: 4 -*- vim:fenc=utf-8:ft=tcl:et:sw=4:ts=4:sts=4\n\n" + body, "# -*- coding: utf-8; mode: python; tab-width: 4; indent-tabs-mode: nil; c-basic-offset: 4 -*- vim:fenc=utf-8:ft=tcl:et:sw=4:ts=4:sts=4\n\n" + body, false},
		{"another filetype", "# -*- coding: utf-8; mode: tcl; tab-width: 4; indent-tabs-mode: nil; c-basic-offset: 4 -*- vim:fenc=utf-8:filetype=python:et:sw=4:ts=4:sts=4\n\n" + body, "# -*- coding: utf-8; mode: tcl; tab-width: 4; indent-tabs-mode: nil; c-basic-offset: 4 -*- vim:fenc=utf-8:filetype=python:et:sw=4:ts=4:sts=4\n\n" + body, false},
		{"tabs", "# -*- coding: utf-8; mode: tcl; tab-width: 8; indent-tabs-mode: t; c-basic-offset: 8 -*- vim:fenc=utf-8:ft=tcl:noet:sw=8:ts=8\n\n" + body, "# -*- coding: utf-8; mode: tcl; tab-width: 8; indent-tabs-mode: t; c-basic-offset: 8 -*- vim:fenc=utf-8:ft=tcl:noet:sw=8:ts=8\n\n" + body, false},
		{"another width", "# vim: set ts=2 sw=2 :\n" + body, "# vim: set ts=2 sw=2 :\n" + body, false},
	} {
		got, changed := WithModeline([]byte(c.src))
		require.Equal(t, c.want, string(got), c.why)
		require.Equal(t, c.changed, changed, c.why)
	}

	for _, form := range []struct {
		name, vim, shortType, longType, softTab string
	}{
		{"spaced vim form", "fenc=utf-8:ft=tcl:et:sw=4:ts=4:sts=4", "ft=tcl", "", "sts=4"},
		{"reordered options", "sts=4:ts=4:sw=4:et:ft=tcl:fenc=utf-8", "ft=tcl", "", "sts=4"},
		{"long option names", "fileencoding=utf-8:filetype=tcl:expandtab:shiftwidth=4:tabstop=4:softtabstop=4", "", "filetype=tcl", "softtabstop=4"},
		{"vim set form", "set fenc=utf-8 ft=tcl et sw=4 ts=4 sts=4 :", "ft=tcl", "", "sts=4"},
	} {
		emacs := "# -*- coding: utf-8; mode: tcl; tab-width: 4; indent-tabs-mode: nil; c-basic-offset: 4 -*-"
		for _, variant := range []struct {
			name, vim string
		}{
			{"missing option", strings.Replace(form.vim, form.softTab, "", 1)},
			{"additional option", strings.Replace(form.vim, form.shortType+form.longType, form.shortType+form.longType+":customoption=1", 1)},
		} {
			t.Run(form.name+"/"+variant.name, func(t *testing.T) {
				src := emacs + " vim: " + variant.vim + "\n\n" + body
				got, changed := WithModeline([]byte(src))
				require.Equal(t, Modeline+"\n\n"+body, string(got))
				require.True(t, changed)
			})
		}
	}
}
