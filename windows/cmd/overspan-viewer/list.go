//go:build windows

package main

import (
	"sort"
	"strings"
	"time"

	"overspan/viewer/internal/client"
	"overspan/viewer/internal/win"
)

// D01: the list of computers (found on the network and saved), the
// add-by-address sheet and the connect sheet.
type listState struct {
	disc      *client.Discovery
	found     map[string]client.Computer
	scanStart time.Time
	cards     map[string]*ctl
	order     []string
	scroll    int32
	maxScroll int32
	btnAdd    *ctl
	btnScan   *ctl

	addEdit *ctl
	addErr  bool

	target   client.Computer
	info     *client.Info
	infoErr  bool
	infoBusy bool
	infoGen  int
	infoRes  struct {
		gen  int
		info *client.Info
	}
	canExtend bool
	mode      string
	full      bool
	segExtend *ctl
	segMirror *ctl
	segFull   *ctl
	segWindow *ctl
	nameEdit  *ctl
	btnGo     *ctl
}

func (a *app) enterList() {
	a.closeSheet()
	a.clearControls()
	a.page = pageList
	a.pageCtls = nil
	a.setTitle(client.TxtAppName)
	l := &a.list
	l.cards = map[string]*ctl{}
	l.scroll = 0
	l.btnScan = a.add(kSecondary, client.TxtRefresh, func() {
		a.mu.Lock()
		a.pending = nil
		a.mu.Unlock()
		l.found = map[string]client.Computer{}
		a.startScan()
	})
	l.btnAdd = a.add(kPrimary, client.TxtAdd, a.openAdd)
	a.pageCtls = []*ctl{l.btnScan, l.btnAdd}
	a.startScan()
	win.SetTimer.Call(a.hwnd, tPrune, 3000, 0)
	a.syncCards()
	win.SetFocus.Call(l.btnAdd.hwnd)
}

func (a *app) leaveList() {
	if a.list.disc != nil {
		a.list.disc.Stop()
		a.list.disc = nil
	}
	win.KillTimer.Call(a.hwnd, tPrune)
}

func (a *app) startScan() {
	l := &a.list
	if l.disc != nil {
		l.disc.Stop()
	}
	if l.found == nil {
		l.found = map[string]client.Computer{}
	}
	l.disc = &client.Discovery{}
	for _, s := range a.prefs.SavedComputers() {
		l.disc.AddHost(s.Host)
	}
	l.scanStart = time.Now()
	l.disc.Start(func(c client.Computer) {
		a.mu.Lock()
		a.pending = append(a.pending, c)
		a.mu.Unlock()
		win.PostMessageW.Call(a.hwnd, wmFound, 0, 0)
	})
	a.syncCards()
}

func (a *app) takeFound() {
	a.mu.Lock()
	got := a.pending
	a.pending = nil
	a.mu.Unlock()
	if a.page != pageList {
		return
	}
	for _, c := range got {
		a.list.found[c.Key()] = c
	}
	a.syncCards()
}

func (a *app) pruneList() {
	if a.page != pageList {
		return
	}
	now := time.Now()
	for k, c := range a.list.found {
		if now.Sub(c.SeenAt) > 9*time.Second {
			delete(a.list.found, k)
		}
	}
	a.syncCards()
}

// computers merges found and saved computers: found first (by name), then
// saved ones that did not answer.
func (a *app) computers() []client.Computer {
	var out []client.Computer
	saved := a.prefs.SavedComputers()
	isSaved := map[string]bool{}
	for _, s := range saved {
		isSaved[s.Key()] = true
	}
	for _, c := range a.list.found {
		c.Saved = isSaved[c.Key()]
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Title()) < strings.ToLower(out[j].Title())
	})
	for _, s := range saved {
		if _, ok := a.list.found[s.Key()]; !ok {
			out = append(out, s)
		}
	}
	return out
}

// syncCards keeps one card control per computer, in list order.
func (a *app) syncCards() {
	if a.page != pageList {
		return
	}
	l := &a.list
	all := a.computers()
	seen := map[string]bool{}
	l.order = l.order[:0]
	for _, c := range all {
		c := c
		k := c.Key()
		seen[k] = true
		l.order = append(l.order, k)
		badge, tone := client.TxtSaved, 0
		if c.Found && c.Enabled {
			badge, tone = client.TxtReady, 1
		} else if c.Found {
			badge, tone = client.TxtPaused, 2
		}
		card := l.cards[k]
		if card == nil {
			card = a.add(kCard, c.Title(), nil)
			l.cards[k] = card
			a.pageCtls = append(a.pageCtls, card)
			if a.sheet != sheetNone {
				card.dimmed = true
				enable(card, false)
			}
		}
		card.onClick = func() { a.openConnect(c) }
		card.sub, card.badge, card.tone = k, badge, tone
		if card.label != c.Title() || win.Text(card.hwnd) != c.Title()+", "+k+", "+badge {
			card.label = c.Title()
			win.SetWindowTextW.Call(card.hwnd, win.S(c.Title()+", "+k+", "+badge))
		}
	}
	for k, card := range l.cards {
		if !seen[k] {
			a.dropControls([]*ctl{card})
			delete(l.cards, k)
			keep := a.pageCtls[:0]
			for _, c := range a.pageCtls {
				if c != card {
					keep = append(keep, c)
				}
			}
			a.pageCtls = keep
		}
	}
	// tab order follows the visual order: header buttons, then cards
	prev := l.btnAdd.hwnd
	win.SetWindowPos.Call(l.btnScan.hwnd, 0, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOACTIVATE)
	win.SetWindowPos.Call(l.btnAdd.hwnd, l.btnScan.hwnd, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOACTIVATE)
	for _, k := range l.order {
		win.SetWindowPos.Call(l.cards[k].hwnd, prev, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOACTIVATE)
		prev = l.cards[k].hwnd
	}
	for _, c := range a.sheetCtls { // a sheet stays after the page in tab order
		win.SetWindowPos.Call(c.hwnd, prev, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOACTIVATE)
		prev = c.hwnd
	}
	a.relayout()
}

func (a *app) layoutList() {
	t := a.th
	c := t.c
	l := &a.list
	cr := a.client()
	gutter := t.px(space5)
	w := min(t.px(contentMax), cr.W()-gutter*2)
	x := (cr.W() - w) / 2
	y := t.px(space6)

	// header: logo, name, tagline; actions on the right (below when narrow)
	ls := t.px(logoSize)
	a.emit(op{k: oLogo, r: rect(x, y, ls, ls)})
	tx := x + ls + t.px(space4)
	bAdd, bScan := a.buttonW(l.btnAdd.label), a.buttonW(l.btnScan.label)
	actionsW := bAdd + bScan + t.px(space2)
	narrow := w-ls-t.px(space4)-actionsW < t.px(260)
	titleW := w - ls - t.px(space4)
	if !narrow {
		titleW -= actionsW + t.px(space4)
	}
	th := t.lineH(fXlBold)
	a.text(rect(tx, y-t.px(space1), titleW, th), client.TxtAppName, fXlBold, c.Text, win.DT_SINGLELINE)
	a.text(rect(tx, y-t.px(space1)+th, titleW, t.lineH(fSm)), client.TxtTagline, fSm, c.Muted, win.DT_SINGLELINE|win.DT_END_ELLIPSIS)
	bh := t.px(controlH)
	by := y + (ls-bh)/2
	if narrow {
		by = y + ls + t.px(space4)
		a.place(l.btnScan, rect(x+w-actionsW, by, bScan, bh), c.Bg)
		a.place(l.btnAdd, rect(x+w-bAdd, by, bAdd, bh), c.Bg)
		y = by + bh
	} else {
		a.place(l.btnScan, rect(x+w-actionsW, by, bScan, bh), c.Bg)
		a.place(l.btnAdd, rect(x+w-bAdd, by, bAdd, bh), c.Bg)
		y += ls
	}

	// section heading and scan status
	y += t.px(space6)
	a.text(rect(x, y, w, t.lineH(fMdBold)), client.TxtTitle, fMdBold, c.Text, win.DT_SINGLELINE)
	y += t.lineH(fMdBold) + t.px(space1)
	scanning := time.Since(l.scanStart) < 6*time.Second
	status := client.FoundCount(len(l.found))
	dot := t.px(space2)
	sx := x
	if scanning || len(l.found) > 0 {
		dc := c.Ok
		if len(l.found) == 0 {
			dc = c.Accent
		}
		lh := t.lineH(fSm)
		a.emit(op{k: oDot, r: rect(x, y+(lh-dot)/2, dot, dot), c: dc})
		sx += dot + t.px(space2)
	}
	a.text(rect(sx, y, w, t.lineH(fSm)), status, fSm, c.Muted, win.DT_SINGLELINE)
	y += t.lineH(fSm) + t.px(space4)

	// cards, scrolled
	top, bottom := y, cr.H()-t.px(space5)
	ch, gap := t.px(cardH), t.px(space2)
	total := int32(len(l.order))*(ch+gap) - gap
	l.maxScroll = max(0, total-(bottom-top))
	l.scroll = max(0, min(l.scroll, l.maxScroll))
	if len(l.order) == 0 {
		a.layoutEmpty(x, top, w)
	}
	for i, k := range l.order {
		card := l.cards[k]
		cy := top + int32(i)*(ch+gap) - l.scroll
		if cy+ch <= top || cy >= bottom {
			a.hide(card)
			continue
		}
		a.place(card, rect(x, cy, w, ch), c.Bg)
	}
	if l.maxScroll > 0 { // scroll hint: a thin bar on the right
		track := bottom - top
		bh := max(t.px(space6), track*track/(total))
		bpos := top + (track-bh)*l.scroll/l.maxScroll
		a.fill(rect(x+w+t.px(space2), bpos, t.px(space1), bh), c.BorderStrong, t.px(space0))
	}
}

func (a *app) layoutEmpty(x, y, w int32) {
	t := a.th
	c := t.c
	pad := t.px(space6)
	inner := w - pad*2
	headH := t.lineH(fMdBold)
	_, bodyH := t.measure(client.TxtNoneBody, fSm, inner)
	ic := t.px(space7)
	h := pad + ic + t.px(space4) + headH + t.px(space2) + bodyH + pad
	box := rect(x, y, w, h)
	a.fill(box, c.Surface, t.px(radiusMd))
	a.strokeR(box, c.Border, t.px(radiusMd), t.px(border))
	cy := y + pad
	icon := rect(x+(w-ic)/2, cy, ic, ic)
	a.fill(icon, c.AccentSoft, ic/2)
	a.emit(op{k: oGlyph, r: inset(icon, t.px(space3)), c: c.Accent})
	cy += ic + t.px(space4)
	a.text(rect(x+pad, cy, inner, headH), client.TxtNoneHead, fMdBold, c.Text, win.DT_CENTER|win.DT_SINGLELINE)
	cy += headH + t.px(space2)
	a.para(x+pad, cy, inner, client.TxtNoneBody, fSm, c.Muted, win.DT_CENTER)
}

func (a *app) scrollList(delta int32) {
	l := &a.list
	if l.maxScroll == 0 {
		return
	}
	l.scroll -= delta * a.th.px(cardH) / 120
	a.relayout()
}

func (a *app) ensureVisible(card *ctl) {
	l := &a.list
	for i, k := range l.order {
		if l.cards[k] != card {
			continue
		}
		t := a.th
		ch, gap := t.px(cardH), t.px(space2)
		// the card's position without scrolling, relative to the list top
		pos := int32(i) * (ch + gap)
		view := a.client().H() // generous: relayout clamps
		if pos < l.scroll {
			l.scroll = pos
		} else if pos+ch > l.scroll+view/2 {
			l.scroll = pos + ch - view/2
		}
		a.relayout()
		return
	}
}

// ------------------------------------------------------------- add sheet

func (a *app) openAdd() {
	l := &a.list
	l.addErr = false
	a.openSheet(sheetAdd, func() {
		l.addEdit = a.add(kEdit, client.TxtAddHint, nil)
		win.SendMessageW.Call(l.addEdit.hwnd, win.EM_SETLIMITTEXT, 120, 0)
		a.add(kSecondary, client.TxtCancel, a.closeSheet)
		ok := a.add(kPrimary, client.TxtAddButton, a.submitAdd)
		a.defID = ok.id
	})
}

func (a *app) submitAdd() {
	l := &a.list
	c, ok := client.ParseAddress(win.Text(l.addEdit.hwnd))
	if !ok {
		l.addErr = true
		a.relayout()
		win.SetFocus.Call(l.addEdit.hwnd)
		win.SendMessageW.Call(l.addEdit.hwnd, win.EM_SETSEL, 0, ^uintptr(0))
		return
	}
	a.closeSheet()
	a.openConnect(c)
}

func (a *app) layoutAdd() {
	t := a.th
	c := t.c
	l := &a.list
	cw := min(t.px(sheetW), a.client().W()-t.px(space4)*2) - t.px(space5)*2
	_, errH := t.measure(client.TxtAddInvalid, fSm, cw)
	h := t.lineH(fLgBold) + t.px(space4) + t.lineH(fSmBold) + t.px(space2) + t.px(controlH)
	if l.addErr {
		h += t.px(space2) + errH
	}
	h += t.px(space5) + t.px(controlH)
	_, in := a.sheetCard(h)
	y := in.Top
	a.text(rect(in.Left, y, in.W(), t.lineH(fLgBold)), client.TxtAddTitle, fLgBold, c.Text, win.DT_SINGLELINE)
	y += t.lineH(fLgBold) + t.px(space4)
	a.text(rect(in.Left, y, in.W(), t.lineH(fSmBold)), client.TxtAddLabel, fSmBold, c.Text, win.DT_SINGLELINE)
	y += t.lineH(fSmBold) + t.px(space2)
	a.field(l.addEdit, rect(in.Left, y, in.W(), t.px(controlH)), c.Surface)
	y += t.px(controlH)
	if l.addErr {
		y += t.px(space2)
		y += a.para(in.Left, y, in.W(), client.TxtAddInvalid, fSm, c.Danger, 0)
	}
	y += t.px(space5)
	a.buttonRow(in.Left, y, in.W(), nil, a.sheetCtls[1], a.sheetCtls[2])
}

// ---------------------------------------------------------- connect sheet

func (a *app) openConnect(target client.Computer) {
	l := &a.list
	for _, s := range a.prefs.SavedComputers() {
		if s.Key() == target.Key() {
			target.Saved = true
			if target.Name == "" {
				target.Name = s.Name
			}
		}
	}
	l.target = target
	l.info, l.infoErr, l.infoBusy = nil, false, true
	l.canExtend = true
	l.mode = a.prefs.Mode
	l.full = a.prefs.Fullscreen
	l.infoGen++
	gen := l.infoGen
	go func() {
		info, err := client.FetchInfo(target)
		if err != nil {
			info = nil
		}
		a.mu.Lock()
		l.infoRes.gen, l.infoRes.info = gen, info
		a.mu.Unlock()
		win.PostMessageW.Call(a.hwnd, wmInfo, 0, 0)
	}()

	a.openSheet(sheetConnect, func() {
		pick := func(mode string) func() {
			return func() { l.mode = mode; a.syncConnect() }
		}
		view := func(full bool) func() {
			return func() { l.full = full; a.syncConnect() }
		}
		l.segExtend = a.add(kSegment, client.TxtModeExtend, pick("extend"))
		l.segMirror = a.add(kSegment, client.TxtModeMirror, pick("mirror"))
		l.segFull = a.add(kSegment, client.TxtViewFull, view(true))
		l.segWindow = a.add(kSegment, client.TxtViewWindow, view(false))
		l.nameEdit = a.add(kEdit, deviceFallbackName(), nil)
		win.SendMessageW.Call(l.nameEdit.hwnd, win.EM_SETLIMITTEXT, 60, 0)
		win.SetWindowTextW.Call(l.nameEdit.hwnd, win.S(a.prefs.DeviceName(deviceFallbackName())))
		if target.Saved {
			a.add(kDangerText, client.TxtForget, func() {
				a.prefs.Forget(l.target)
				a.closeSheet()
				a.syncCards()
			})
		}
		a.add(kSecondary, client.TxtCancel, a.closeSheet)
		l.btnGo = a.add(kPrimary, client.TxtConnect, a.submitConnect)
		enable(l.btnGo, false)
		a.defID = l.btnGo.id
	})
	a.syncConnect()
}

func (a *app) syncConnect() {
	l := &a.list
	if !l.canExtend {
		l.mode = "mirror"
	}
	l.segExtend.on, l.segMirror.on = l.mode == "extend", l.mode == "mirror"
	l.segFull.on, l.segWindow.on = l.full, !l.full
	enable(l.segExtend, l.canExtend)
	a.relayout()
}

func (a *app) infoArrived() {
	l := &a.list
	a.mu.Lock()
	gen, info := l.infoRes.gen, l.infoRes.info
	a.mu.Unlock()
	if gen != l.infoGen || a.sheet != sheetConnect {
		return
	}
	l.infoBusy = false
	l.info = info
	l.infoErr = info == nil
	if info != nil {
		if info.Name != "" {
			l.target.Name = info.Name
		}
		l.canExtend = info.CanExtend
		enable(l.btnGo, info.Enabled)
	} else {
		enable(l.btnGo, true) // the info call may be blocked; let the person try anyway
	}
	a.syncConnect()
}

func (a *app) submitConnect() {
	l := &a.list
	name := strings.TrimSpace(win.Text(l.nameEdit.hwnd))
	if name != "" && name != deviceFallbackName() {
		a.prefs.Name = name
	} else {
		a.prefs.Name = ""
	}
	a.prefs.Mode = l.mode
	a.prefs.Fullscreen = l.full
	a.prefs.Remember(l.target)
	target := l.target
	a.closeSheet()
	a.enterDisplay(target, l.mode)
}

func (a *app) layoutConnect() {
	t := a.th
	c := t.c
	l := &a.list
	cw := min(t.px(sheetW), a.client().W()-t.px(space4)*2) - t.px(space5)*2

	status, scol := client.TxtChecking, c.Muted
	switch {
	case l.infoBusy:
	case l.infoErr:
		status, scol = client.TxtUnreachable, c.Danger
	case l.info != nil && !l.info.Enabled:
		status, scol = client.TxtPausedText, c.Warn
	case l.info != nil && l.info.PIN:
		status, scol = client.TxtReady+" · "+client.TxtPINLabel, c.Ok
	default:
		status, scol = client.TxtReady, c.Ok
	}
	dot := t.px(space2)
	_, statusH := t.measure(status, fSm, cw-dot-t.px(space2))
	hint := client.TxtModeExtendTip
	if !l.canExtend {
		hint = client.TxtModeNoExtend
	} else if l.mode == "mirror" {
		hint = client.TxtModeMirrorTip
	}
	_, hintH := t.measure(hint, fSm, cw)
	lab := t.lineH(fSmBold)
	ctlH := t.px(controlH)
	h := t.lineH(fLgBold) + t.lineH(fSm) + t.px(space3) + statusH +
		t.px(space5) + lab + t.px(space2) + ctlH + t.px(space2) + hintH +
		t.px(space4) + lab + t.px(space2) + ctlH +
		t.px(space4) + lab + t.px(space2) + ctlH + t.px(space1) + t.lineH(fXs) +
		t.px(space5) + ctlH
	_, in := a.sheetCard(h)
	x, y, w := in.Left, in.Top, in.W()

	a.text(rect(x, y, w, t.lineH(fLgBold)), l.target.Title(), fLgBold, c.Text, win.DT_SINGLELINE|win.DT_END_ELLIPSIS)
	y += t.lineH(fLgBold)
	a.text(rect(x, y, w, t.lineH(fSm)), l.target.Key(), fSm, c.Muted, win.DT_SINGLELINE)
	y += t.lineH(fSm) + t.px(space3)
	a.emit(op{k: oDot, r: rect(x, y+(t.lineH(fSm)-dot)/2, dot, dot), c: scol})
	a.para(x+dot+t.px(space2), y, w-dot-t.px(space2), status, fSm, scol, 0)
	y += statusH + t.px(space5)

	seg := func(label string, left, right *ctl, after string) {
		a.text(rect(x, y, w, lab), label, fSmBold, c.Text, win.DT_SINGLELINE)
		y += lab + t.px(space2)
		half := (w - t.px(space2)) / 2
		a.place(left, rect(x, y, half, ctlH), c.Surface)
		a.place(right, rect(x+half+t.px(space2), y, w-half-t.px(space2), ctlH), c.Surface)
		y += ctlH
		if after != "" {
			y += t.px(space2)
			y += a.para(x, y, w, after, fSm, c.Muted, 0)
		}
	}
	seg(client.TxtModeQuestion, l.segExtend, l.segMirror, hint)
	y += t.px(space4)
	seg(client.TxtViewQuestion, l.segFull, l.segWindow, "")
	y += t.px(space4)
	a.text(rect(x, y, w, lab), client.TxtDeviceName, fSmBold, c.Text, win.DT_SINGLELINE)
	y += lab + t.px(space2)
	a.field(l.nameEdit, rect(x, y, w, ctlH), c.Surface)
	y += ctlH + t.px(space1)
	a.text(rect(x, y, w, t.lineH(fXs)), client.TxtDeviceTip, fXs, c.Muted, win.DT_SINGLELINE)
	y += t.lineH(fXs) + t.px(space5)

	n := len(a.sheetCtls)
	var lead *ctl
	if l.target.Saved {
		lead = a.sheetCtls[n-3]
	}
	a.buttonRow(x, y, w, lead, a.sheetCtls[n-2], a.sheetCtls[n-1])
}
