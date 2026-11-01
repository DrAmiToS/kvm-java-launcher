package gui

import (
	"bufio"
	"fmt"
	"image/color"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type connection struct {
	Name     string
	Group    string
	Target   string
	Host     string
	User     string
	Password string
	Profile  string
	CertMode string
	TimeMode string
	JNLPPath string
	Notes    string
	Badges   []string
}

type bookRow struct {
	Header string
	Index  int
}

type ui struct {
	app    fyne.App
	window fyne.Window

	name        *widget.Entry
	group       *widget.Entry
	target      *widget.Entry
	host        *widget.Entry
	user        *widget.Entry
	password    *widget.Entry
	profile     *widget.Select
	certMode    *widget.Select
	timeMode    *widget.Select
	jnlpPath    *widget.Entry
	jnlpRow     fyne.CanvasObject
	notes       *widget.Entry
	search      *widget.Entry
	log         *widget.TextGrid
	connect     *widget.Button
	themeMode   *widget.Select
	scale       *widget.Select
	list        *widget.List
	status      *widget.Label
	selectedIdx int
	themeChoice string
	scaleVal    float32
	dirtyFocus  bool
	bookPath    string
	bookPass    string
	bookOpen    bool
	bookLocked  bool
	running     bool
	cmdMu       sync.Mutex
	childCmd    *exec.Cmd
	logMu       sync.Mutex
	logText     strings.Builder
	items       []connection
	rows        []bookRow
	settingForm bool
}

func Run() error {
	a := app.NewWithID("ru.kvm-java-launcher")
	a.SetIcon(appIcon)

	bookPath, err := defaultBookPath()
	if err != nil {
		bookPath = ""
	}
	u := &ui{
		app:         a,
		themeChoice: "auto",
		scaleVal:    1,
		selectedIdx: -1,
		bookPath:    bookPath,
	}
	u.applyTheme()
	u.rebuildRows("")
	u.window = a.NewWindow("KVM Java Launcher")
	u.window.SetIcon(appIcon)
	u.window.Resize(fyne.NewSize(1220, 860))
	u.window.SetContent(u.build())
	u.window.Show()
	if u.bookPath != "" && bookExists(u.bookPath) {
		u.promptUnlockBook(nil)
	}
	a.Run()
	return nil
}

func (u *ui) build() fyne.CanvasObject {
	u.name = widget.NewEntry()
	u.group = widget.NewEntry()
	u.target = widget.NewEntry()
	u.host = widget.NewEntry()
	u.user = widget.NewEntry()
	u.password = widget.NewPasswordEntry()
	u.profile = widget.NewSelect([]string{"auto", "ami", "supermicro", "supermicro-sol", "dell", "hp", "lenovo", "generic", "jnlp"}, func(_ string) {
		u.updateJNLPVisibility()
		u.handleFormChanged()
	})
	u.profile.SetSelected("auto")
	u.certMode = widget.NewSelect([]string{"default", "noverify", "renew-client", "direct", "off"}, func(_ string) {
		u.handleFormChanged()
	})
	u.certMode.SetSelected("default")
	u.timeMode = widget.NewSelect([]string{"off", "auto"}, func(_ string) {
		u.handleFormChanged()
	})
	u.timeMode.SetSelected("off")
	u.jnlpPath = widget.NewEntry()
	u.notes = widget.NewMultiLineEntry()
	u.notes.Wrapping = fyne.TextWrapWord
	u.notes.SetMinRowsVisible(1)
	u.notes.OnChanged = func(text string) {
		u.updateNotesHeight(text)
		u.handleFormChanged()
	}
	u.watchEntryChanges(u.name, u.group, u.target, u.host, u.user, u.password, u.jnlpPath)

	u.log = widget.NewTextGrid()
	u.log.Scroll = fyne.ScrollVerticalOnly

	u.connect = widget.NewButtonWithIcon("Connect", theme.MediaPlayIcon(), u.connectPressed)
	u.connect.Importance = widget.SuccessImportance
	test := widget.NewButtonWithIcon("Test", theme.ConfirmIcon(), func() {
		dialog.ShowInformation("KVM Java Launcher", "Test mode will use the same connection path as Connect in a later step.", u.window)
	})
	save := widget.NewButtonWithIcon("Save", theme.DocumentSaveIcon(), u.savePressed)
	saveAs := widget.NewButton("Save as New", func() {
		u.saveAsNewPressed()
	})
	browse := widget.NewButtonWithIcon("Browse JNLP", theme.FolderOpenIcon(), func() {
		dialog.ShowFileOpen(func(r fyne.URIReadCloser, err error) {
			if err != nil {
				dialog.ShowError(err, u.window)
				return
			}
			if r == nil {
				return
			}
			defer r.Close()
			u.jnlpPath.SetText(r.URI().Path())
			if u.profile.Selected == "auto" {
				u.profile.SetSelected("jnlp")
			}
		}, u.window)
	})

	u.themeMode = widget.NewSelect([]string{"auto", "light", "dark"}, func(value string) {
		u.themeChoice = value
		u.applyTheme()
	})
	u.themeMode.SetSelected("auto")
	u.scale = widget.NewSelect(scaleOptions(), func(value string) {
		u.applyScale(value)
	})
	u.scale.SetSelected("100%")

	top := container.NewBorder(nil, nil, widget.NewLabelWithStyle("KVM Java Launcher", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), nil,
		container.NewHBox(layout.NewSpacer(), widget.NewLabel("Theme"), u.themeMode, widget.NewLabel("Scale"), u.scale))

	formGrid := container.NewGridWithColumns(2,
		u.field("Name", u.name, func(cur, saved connection) bool { return cur.Name != saved.Name }),
		u.field("Group", u.group, func(cur, saved connection) bool { return cur.Group != saved.Group }),
		u.field("BMC IP / URL", u.target, func(cur, saved connection) bool { return cur.Target != saved.Target }),
		u.field("Host header for NAT", u.host, func(cur, saved connection) bool { return cur.Host != saved.Host }),
		u.field("User", u.user, func(cur, saved connection) bool { return cur.User != saved.User }),
		u.field("Password", u.password, func(cur, saved connection) bool { return cur.Password != saved.Password }),
	)
	modeRow := container.NewAdaptiveGrid(3,
		u.field("Profile", u.profile, func(cur, saved connection) bool { return cur.Profile != saved.Profile }),
		u.field("Cert mode", u.certMode, func(cur, saved connection) bool { return cur.CertMode != saved.CertMode }),
		u.field("Time workaround", u.timeMode, func(cur, saved connection) bool { return cur.TimeMode != saved.TimeMode }),
	)
	u.jnlpRow = u.field("Local JNLP", container.NewBorder(nil, nil, nil, browse, u.jnlpPath), func(cur, saved connection) bool {
		return cur.JNLPPath != saved.JNLPPath
	})
	u.updateJNLPVisibility()
	notes := u.field("Notes", u.notes, func(cur, saved connection) bool { return cur.Notes != saved.Notes })
	buttons := container.NewHBox(saveAs, save, test, u.connect)
	clearConnection := widget.NewButtonWithIcon("", theme.ContentAddIcon(), u.clearForm)
	connectionHeader := container.NewHBox(
		widget.NewLabelWithStyle("Connection", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		clearConnection,
	)
	formContent := container.NewVBox(
		top,
		widget.NewSeparator(),
		connectionHeader,
		formGrid,
		modeRow,
		u.jnlpRow,
		notes,
		buttons,
	)
	formScroll := container.NewVScroll(formContent)
	copyLog := widget.NewButtonWithIcon("", theme.ContentCopyIcon(), func() {
		u.logMu.Lock()
		value := u.logText.String()
		u.logMu.Unlock()
		u.window.Clipboard().SetContent(value)
	})
	logHeader := container.NewBorder(nil, nil, widget.NewLabelWithStyle("Launch Log", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), copyLog)
	logContent := container.NewBorder(logHeader, nil, nil, nil, u.log)
	left := container.New(&formLogLayout{formContent: formContent, logMin: 150, gap: 10}, formScroll, logContent)

	right := u.buildBook()
	split := container.NewHSplit(left, right)
	split.Offset = 0.54
	return container.New(&fixedMinLayout{min: fyne.NewSize(600, 300)}, split)
}

func (u *ui) buildBook() fyne.CanvasObject {
	u.search = widget.NewEntry()
	u.search.SetPlaceHolder("Search saved connections")
	u.search.OnChanged = func(_ string) {
		u.applyFilter()
	}
	u.status = widget.NewLabel("")
	u.updateStatus()

	u.list = widget.NewList(
		func() int { return len(u.rows) },
		func() fyne.CanvasObject {
			bg := canvas.NewRectangle(color.Transparent)
			title := widget.NewLabelWithStyle("Connection", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
			meta := widget.NewLabel("target    user    profile")
			badges := widget.NewLabel("options")
			open := widget.NewButtonWithIcon("", theme.NavigateNextIcon(), nil)
			remove := widget.NewButton("×", nil)
			remove.Importance = widget.DangerImportance
			titleRow := container.NewBorder(nil, nil, nil, remove, title)
			text := container.NewVBox(titleRow, meta, badges)
			content := container.NewBorder(nil, nil, nil, open, text)
			return container.NewStack(bg, content)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			rowData := u.rows[id]
			stack := obj.(*fyne.Container)
			bg := stack.Objects[0].(*canvas.Rectangle)
			row := stack.Objects[1].(*fyne.Container)
			text := row.Objects[0].(*fyne.Container)
			titleRow := text.Objects[0].(*fyne.Container)
			title := titleRow.Objects[0].(*widget.Label)
			remove := titleRow.Objects[1].(*widget.Button)
			meta := text.Objects[1].(*widget.Label)
			badges := text.Objects[2].(*widget.Label)
			open := row.Objects[1].(*widget.Button)
			u.applyRowHighlight(bg, rowData)
			if rowData.Header != "" {
				title.SetText(rowData.Header)
				meta.SetText("")
				badges.SetText("")
				remove.Hide()
				open.Hide()
				return
			}
			remove.Show()
			open.Show()
			c := u.items[rowData.Index]
			title.SetText(c.Name)
			meta.SetText(strings.TrimSpace(strings.Join([]string{c.Target, c.User, c.Profile}, "    ")))
			badges.SetText(strings.Join(c.Badges, "    "))
			open.OnTapped = func() {
				u.loadConnection(rowData.Index)
			}
			remove.OnTapped = func() {
				u.deleteConnectionPressed(rowData.Index)
			}
		},
	)
	u.list.OnSelected = func(id widget.ListItemID) {
		rowData := u.rows[id]
		if rowData.Header == "" {
			u.loadConnection(rowData.Index)
		}
		u.list.Unselect(id)
	}
	u.applyListHeights()

	actions := container.NewHBox(
		widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), func() { u.applyFilter() }),
		widget.NewButtonWithIcon("", theme.ContentAddIcon(), func() { u.clearForm() }),
	)
	header := container.NewBorder(nil, nil, nil, actions, u.search)
	return container.NewBorder(header, u.status, nil, nil, u.list)
}

func (u *ui) field(label string, obj fyne.CanvasObject, changed func(current, saved connection) bool) fyne.CanvasObject {
	_ = changed
	return container.New(&compactFieldLayout{gap: -7, labelYOffset: 4}, widget.NewLabel(label), obj)
}

func (u *ui) watchEntryChanges(entries ...*widget.Entry) {
	for _, entry := range entries {
		entry.OnChanged = func(string) {
			u.handleFormChanged()
		}
	}
}

func (u *ui) handleFormChanged() {
	if u.settingForm {
		return
	}
	if u.name == nil || u.group == nil || u.target == nil || u.host == nil || u.user == nil || u.password == nil ||
		u.profile == nil || u.certMode == nil || u.timeMode == nil || u.jnlpPath == nil || u.notes == nil {
		return
	}
	u.refreshSelectionState()
}

func (u *ui) refreshSelectionState() {
	dirty := u.selectedDirty()
	if u.dirtyFocus != dirty {
		u.dirtyFocus = dirty
		u.applyTheme()
	}
	if u.list != nil {
		u.list.Refresh()
		u.applyListHeights()
	}
	u.updateStatus()
}

func (u *ui) connectPressed() {
	if u.running {
		u.closeSession()
		return
	}
	args, err := u.launchArgs()
	if err != nil {
		dialog.ShowError(err, u.window)
		return
	}
	u.running = true
	u.setRunningState(true)
	u.setLog("")
	u.appendLog("Starting launcher...\n")
	u.appendLog("Command: KVM-Java-Launcher.exe " + redactArgsForLog(args) + "\n\n")
	go u.runChild(args)
}

func (u *ui) launchArgs() ([]string, error) {
	target := strings.TrimSpace(u.target.Text)
	hostHeader := strings.TrimSpace(u.host.Text)
	user := strings.TrimSpace(u.user.Text)
	password := u.password.Text
	profile := strings.TrimSpace(u.profile.Selected)
	certMode := strings.TrimSpace(u.certMode.Selected)
	timeMode := strings.TrimSpace(u.timeMode.Selected)
	jnlpPath := strings.TrimSpace(u.jnlpPath.Text)

	if profile == "" {
		profile = "auto"
	}
	if target == "" && jnlpPath == "" {
		return nil, fmt.Errorf("BMC IP / URL or Local JNLP is required")
	}
	args := []string{"-no-pause"}
	if target != "" {
		args = append(args, "-target", target)
	}
	if hostHeader != "" {
		args = append(args, "-host-header", hostHeader)
	}
	if user != "" {
		args = append(args, "-user", user)
	}
	if password != "" {
		args = append(args, "-password", password)
	}
	args = append(args, "-profile", profile)
	if jnlpPath != "" {
		args = append(args, "-jnlp", jnlpPath)
	}
	if certMode != "" && certMode != "default" {
		args = append(args, "-supermicro-cert-mode", certMode)
	}
	if timeMode != "" && timeMode != "off" {
		args = append(args, "-supermicro-time-workaround", timeMode)
	}
	return args, nil
}

func (u *ui) savePressed() {
	u.ensureBookReady(func() {
		c, err := u.formConnection()
		if err != nil {
			dialog.ShowError(err, u.window)
			return
		}
		if u.selectedIdx >= 0 && u.selectedIdx < len(u.items) {
			u.items[u.selectedIdx] = c
		} else {
			u.items = append(u.items, c)
			u.selectedIdx = len(u.items) - 1
		}
		u.persistBook("Connection saved.")
	})
}

func (u *ui) saveAsNewPressed() {
	u.ensureBookReady(func() {
		c, err := u.formConnection()
		if err != nil {
			dialog.ShowError(err, u.window)
			return
		}
		u.items = append(u.items, c)
		u.selectedIdx = len(u.items) - 1
		u.persistBook("Connection saved as new.")
	})
}

func (u *ui) formConnection() (connection, error) {
	c := u.currentFormConnection()
	if c.Target == "" && c.JNLPPath == "" {
		return connection{}, fmt.Errorf("BMC IP / URL or Local JNLP is required")
	}
	if c.Name == "" {
		c.Name = defaultString(c.Target, c.JNLPPath)
	}
	c.Badges = connectionBadges(c)
	return c, nil
}

func (u *ui) currentFormConnection() connection {
	c := connection{
		Name:     strings.TrimSpace(u.name.Text),
		Group:    strings.TrimSpace(u.group.Text),
		Target:   strings.TrimSpace(u.target.Text),
		Host:     strings.TrimSpace(u.host.Text),
		User:     strings.TrimSpace(u.user.Text),
		Password: u.password.Text,
		Profile:  strings.TrimSpace(u.profile.Selected),
		CertMode: strings.TrimSpace(u.certMode.Selected),
		TimeMode: strings.TrimSpace(u.timeMode.Selected),
		JNLPPath: strings.TrimSpace(u.jnlpPath.Text),
		Notes:    strings.TrimSpace(u.notes.Text),
	}
	if c.Profile == "" {
		c.Profile = "auto"
	}
	if c.CertMode == "" {
		c.CertMode = "default"
	}
	if c.TimeMode == "" {
		c.TimeMode = "off"
	}
	return c
}

func connectionBadges(c connection) []string {
	var badges []string
	if c.Host != "" {
		badges = append(badges, "host header")
	}
	if c.CertMode != "" && c.CertMode != "default" {
		badges = append(badges, c.CertMode)
	}
	if c.TimeMode != "" && c.TimeMode != "off" {
		badges = append(badges, "time "+c.TimeMode)
	}
	if c.Password != "" {
		badges = append(badges, "password saved")
	}
	if c.JNLPPath != "" {
		badges = append(badges, "local file")
	}
	return badges
}

func (u *ui) selectedDirty() bool {
	if u.selectedIdx < 0 || u.selectedIdx >= len(u.items) {
		return false
	}
	return !sameConnectionFields(u.currentFormConnection(), normalizeConnection(u.items[u.selectedIdx]))
}

func sameConnectionFields(a, b connection) bool {
	return a.Name == b.Name &&
		a.Group == b.Group &&
		a.Target == b.Target &&
		a.Host == b.Host &&
		a.User == b.User &&
		a.Password == b.Password &&
		a.Profile == b.Profile &&
		a.CertMode == b.CertMode &&
		a.TimeMode == b.TimeMode &&
		a.JNLPPath == b.JNLPPath &&
		a.Notes == b.Notes
}

func normalizeConnection(c connection) connection {
	c.Name = strings.TrimSpace(c.Name)
	c.Group = strings.TrimSpace(c.Group)
	c.Target = strings.TrimSpace(c.Target)
	c.Host = strings.TrimSpace(c.Host)
	c.User = strings.TrimSpace(c.User)
	c.Profile = defaultString(strings.TrimSpace(c.Profile), "auto")
	c.CertMode = defaultString(strings.TrimSpace(c.CertMode), "default")
	c.TimeMode = defaultString(strings.TrimSpace(c.TimeMode), "off")
	c.JNLPPath = strings.TrimSpace(c.JNLPPath)
	c.Notes = strings.TrimSpace(c.Notes)
	return c
}

func (u *ui) ensureBookReady(next func()) {
	if u.bookLocked {
		dialog.ShowInformation("Connection book", "Connection book was not unlocked. Saving is disabled until the application is restarted.", u.window)
		return
	}
	if u.bookOpen {
		next()
		return
	}
	if u.bookPath == "" {
		dialog.ShowError(fmt.Errorf("connection book path is unavailable"), u.window)
		return
	}
	if bookExists(u.bookPath) {
		u.promptUnlockBook(next)
		return
	}
	u.promptCreateBook(next)
}

func (u *ui) promptUnlockBook(next func()) {
	password := widget.NewPasswordEntry()
	errorText := widget.NewLabel("")
	errorText.Wrapping = fyne.TextWrapWord
	errorText.Hide()
	form := widget.NewForm(widget.NewFormItem("Master password", password))
	content := container.NewVBox(form, errorText)
	d := dialog.NewCustomWithoutButtons("Unlock connection book", content, u.window)
	unlock := widget.NewButton("Unlock", func() {
		book, err := loadConnectionBook(u.bookPath, password.Text)
		if err != nil {
			errorText.SetText("Could not unlock connection book: wrong password or damaged file.")
			errorText.Show()
			errorText.Refresh()
			d.Refresh()
			return
		}
		u.items = append([]connection(nil), book.Items...)
		u.bookPass = password.Text
		u.bookOpen = true
		u.bookLocked = false
		u.selectedIdx = -1
		u.applyFilter()
		d.Hide()
		if next != nil {
			next()
		}
	})
	cancel := widget.NewButton("Cancel", func() {
		u.bookLocked = true
		u.updateStatus()
		d.Hide()
	})
	d.SetButtons([]fyne.CanvasObject{layout.NewSpacer(), cancel, unlock})
	d.Resize(fyne.NewSize(430, 150))
	d.Show()
}

func (u *ui) promptCreateBook(next func()) {
	password := widget.NewPasswordEntry()
	confirm := widget.NewPasswordEntry()
	errorText := widget.NewLabel("")
	errorText.Wrapping = fyne.TextWrapWord
	errorText.Hide()
	form := widget.NewForm(
		widget.NewFormItem("Master password", password),
		widget.NewFormItem("Confirm password", confirm),
	)
	content := container.NewVBox(form, errorText)
	d := dialog.NewCustomWithoutButtons("Create encrypted connection book", content, u.window)
	create := widget.NewButton("Create", func() {
		switch {
		case len(password.Text) < 8:
			errorText.SetText("Master password must be at least 8 characters.")
		case password.Text != confirm.Text:
			errorText.SetText("Master passwords do not match.")
		default:
			u.bookPass = password.Text
			u.bookOpen = true
			u.bookLocked = false
			u.updateStatus()
			d.Hide()
			if next != nil {
				next()
			}
			return
		}
		errorText.Show()
		errorText.Refresh()
		d.Refresh()
	})
	cancel := widget.NewButton("Cancel", func() {
		d.Hide()
	})
	d.SetButtons([]fyne.CanvasObject{layout.NewSpacer(), cancel, create})
	d.Resize(fyne.NewSize(460, 180))
	d.Show()
}

func (u *ui) persistBook(message string) {
	if err := saveConnectionBook(u.bookPath, u.bookPass, u.items); err != nil {
		dialog.ShowError(err, u.window)
		return
	}
	u.applyFilter()
	u.refreshSelectionState()
	u.appendLog(message + "\n")
}

func (u *ui) deleteConnectionPressed(index int) {
	if index < 0 || index >= len(u.items) {
		return
	}
	c := u.items[index]
	name := defaultString(c.Name, defaultString(c.Target, c.JNLPPath))
	message := widget.NewLabel(fmt.Sprintf("Delete connection %q?", name))
	message.Wrapping = fyne.TextWrapWord
	confirm := widget.NewButton("Delete (5)", nil)
	confirm.Disable()
	cancel := widget.NewButton("Cancel", nil)
	content := container.NewVBox(message)
	d := dialog.NewCustomWithoutButtons("Delete connection", content, u.window)

	done := make(chan struct{})
	cancel.OnTapped = func() {
		close(done)
		d.Hide()
	}
	confirm.OnTapped = func() {
		close(done)
		d.Hide()
		u.deleteConnection(index)
	}
	d.SetOnClosed(func() {
		select {
		case <-done:
		default:
			close(done)
		}
	})
	d.SetButtons([]fyne.CanvasObject{layout.NewSpacer(), cancel, confirm})
	d.Resize(fyne.NewSize(440, 130))
	d.Show()

	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for remaining := 5; remaining > 0; remaining-- {
			fyne.Do(func() {
				confirm.SetText(fmt.Sprintf("Delete (%d)", remaining))
				confirm.Disable()
			})
			select {
			case <-done:
				return
			case <-ticker.C:
			}
		}
		fyne.Do(func() {
			confirm.SetText("Delete")
			confirm.Enable()
		})
	}()
}

func (u *ui) deleteConnection(index int) {
	if index < 0 || index >= len(u.items) {
		return
	}
	deleted := u.items[index]
	u.items = append(u.items[:index], u.items[index+1:]...)
	if u.selectedIdx == index {
		u.clearForm()
	} else if u.selectedIdx > index {
		u.selectedIdx--
	}
	if u.bookOpen {
		u.persistBook(fmt.Sprintf("Connection deleted: %s", defaultString(deleted.Name, deleted.Target)))
		return
	}
	u.applyFilter()
}

func (u *ui) runChild(args []string) {
	exe, err := os.Executable()
	if err != nil {
		u.finishRun(fmt.Errorf("could not locate launcher exe: %w", err))
		return
	}
	cmd := exec.Command(exe, args...)
	if runtime.GOOS == "windows" {
		cmd.SysProcAttr = hideChildWindow()
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		u.finishRun(err)
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		u.finishRun(err)
		return
	}
	if err := cmd.Start(); err != nil {
		u.finishRun(err)
		return
	}
	u.cmdMu.Lock()
	u.childCmd = cmd
	u.cmdMu.Unlock()
	done := make(chan struct{}, 2)
	go u.copyOutput(stdout, done)
	go u.copyOutput(stderr, done)
	<-done
	<-done
	err = cmd.Wait()
	u.cmdMu.Lock()
	if u.childCmd == cmd {
		u.childCmd = nil
	}
	u.cmdMu.Unlock()
	u.finishRun(err)
}

func (u *ui) copyOutput(r io.Reader, done chan<- struct{}) {
	defer func() { done <- struct{}{} }()
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		u.appendLog(scanner.Text() + "\n")
	}
	if err := scanner.Err(); err != nil {
		u.appendLog("Log stream error: " + err.Error() + "\n")
	}
}

func (u *ui) finishRun(err error) {
	if err != nil {
		u.appendLog("\nERROR: " + err.Error() + "\n")
	} else {
		u.appendLog("\nJava KVM session finished.\n")
	}
	fyne.Do(func() {
		u.running = false
		u.setRunningState(false)
	})
}

func (u *ui) setRunningState(running bool) {
	u.running = running
	if running {
		u.connect.SetText("Close")
		u.connect.SetIcon(theme.MediaStopIcon())
		u.connect.Importance = widget.WarningImportance
		u.connect.Refresh()
		return
	}
	u.connect.SetText("Connect")
	u.connect.SetIcon(theme.MediaPlayIcon())
	u.connect.Importance = widget.SuccessImportance
	u.connect.Refresh()
}

func (u *ui) closeSession() {
	u.cmdMu.Lock()
	cmd := u.childCmd
	u.cmdMu.Unlock()
	if cmd == nil || cmd.Process == nil {
		u.appendLog("Close requested, but no launcher process is active.\n")
		return
	}
	u.appendLog("Closing Java KVM session...\n")
	if err := terminateProcessTree(cmd); err != nil {
		u.appendLog("Close failed: " + err.Error() + "\n")
	}
}

func (u *ui) setLog(text string) {
	u.logMu.Lock()
	u.logText.Reset()
	u.logText.WriteString(text)
	value := u.logText.String()
	u.logMu.Unlock()
	fyne.Do(func() {
		u.log.SetText(value)
		u.log.ScrollToTop()
	})
}

func (u *ui) appendLog(text string) {
	u.logMu.Lock()
	u.logText.WriteString(text)
	value := u.logText.String()
	u.logMu.Unlock()
	fyne.Do(func() {
		u.log.SetText(value)
		u.log.ScrollToBottom()
	})
}

func (u *ui) applyScale(value string) {
	if value == "custom..." {
		d := dialog.NewEntryDialog("Custom scale", "Scale percent", func(text string) {
			if err := u.setScalePercent(text); err != nil {
				dialog.ShowError(err, u.window)
			}
		}, u.window)
		d.SetText(fmt.Sprintf("%.0f%%", u.scaleVal*100))
		d.Show()
		return
	}
	if err := u.setScalePercent(value); err != nil {
		dialog.ShowError(err, u.window)
	}
}

func (u *ui) setScalePercent(value string) error {
	value = strings.TrimSuffix(value, "%")
	n, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("invalid scale %q", value)
	}
	if n < 50 || n > 200 {
		return fmt.Errorf("scale must be between 50%% and 200%%")
	}
	u.scaleVal = float32(n) / 100
	selected := fmt.Sprintf("%d%%", n)
	if !stringInSlice(u.scale.Options, selected) {
		u.scale.Options = append([]string{selected}, u.scale.Options...)
		u.scale.Refresh()
	}
	if u.scale.Selected != selected {
		u.scale.SetSelected(selected)
	}
	u.applyTheme()
	return nil
}

func (u *ui) applyTheme() {
	base := theme.DefaultTheme()
	switch u.themeChoice {
	case "light":
		base = theme.LightTheme()
	case "dark":
		base = theme.DarkTheme()
	}
	u.app.Settings().SetTheme(scaledTheme{base: base, scale: u.scaleVal, dirtyFocus: u.dirtyFocus})
}

func (u *ui) applyFilter() {
	query := strings.ToLower(strings.TrimSpace(u.search.Text))
	u.rebuildRows(query)
	u.list.Refresh()
	u.applyListHeights()
	u.updateStatus()
}

func (u *ui) applyListHeights() {
	if u.list == nil {
		return
	}
	for i, row := range u.rows {
		if row.Header != "" {
			u.list.SetItemHeight(widget.ListItemID(i), 42)
		} else {
			u.list.SetItemHeight(widget.ListItemID(i), 112)
		}
	}
}

func (u *ui) applyRowHighlight(bg *canvas.Rectangle, row bookRow) {
	bg.FillColor = color.Transparent
	bg.StrokeColor = color.Transparent
	bg.StrokeWidth = 0
	if row.Header != "" {
		bg.Refresh()
		return
	}
	if row.Index == u.selectedIdx {
		bg.FillColor = color.NRGBA{R: 0x4d, G: 0x73, B: 0x93, A: 0x42}
		bg.StrokeColor = color.NRGBA{R: 0x78, G: 0xa6, B: 0xc8, A: 0xb8}
		bg.StrokeWidth = 1
	}
	bg.Refresh()
}

func (u *ui) rebuildRows(query string) {
	u.rows = u.rows[:0]
	currentGroup := ""
	for i, c := range u.items {
		if query != "" && !connectionMatches(c, query) {
			continue
		}
		group := defaultString(c.Group, "Ungrouped")
		if group != currentGroup {
			currentGroup = group
			u.rows = append(u.rows, bookRow{Header: group, Index: -1})
		}
		u.rows = append(u.rows, bookRow{Index: i})
	}
}

func connectionMatches(c connection, query string) bool {
	haystack := strings.ToLower(strings.Join([]string{
		c.Name, c.Group, c.Target, c.Host, c.User, c.Profile, c.JNLPPath, strings.Join(c.Badges, " "), c.Notes,
	}, " "))
	return strings.Contains(haystack, query)
}

func (u *ui) updateStatus() {
	if u.status == nil {
		return
	}
	details := fmt.Sprintf("%d / %d connections", u.visibleConnectionCount(), len(u.items))
	switch {
	case u.bookLocked:
		details += "    connection book locked"
	case u.bookOpen:
		details += "    encrypted book open"
	case u.bookPath != "":
		details += "    no connection book"
	default:
		details += "    storage unavailable"
	}
	if u.selectedIdx >= 0 && u.selectedIdx < len(u.items) {
		details += "    selected"
		if u.selectedDirty() {
			details += "    unsaved changes"
		}
	}
	u.status.SetText(details)
}

func (u *ui) visibleConnectionCount() int {
	n := 0
	for _, row := range u.rows {
		if row.Header == "" {
			n++
		}
	}
	return n
}

func (u *ui) loadConnection(index int) {
	if index < 0 || index >= len(u.items) {
		return
	}
	u.settingForm = true
	defer func() {
		u.settingForm = false
		u.refreshSelectionState()
	}()
	u.selectedIdx = index
	c := u.items[index]
	u.name.SetText(c.Name)
	u.group.SetText(c.Group)
	u.target.SetText(c.Target)
	u.host.SetText(c.Host)
	u.user.SetText(c.User)
	u.password.SetText(c.Password)
	u.profile.SetSelected(defaultString(c.Profile, "auto"))
	u.certMode.SetSelected(defaultString(c.CertMode, "default"))
	u.timeMode.SetSelected(defaultString(c.TimeMode, "off"))
	u.jnlpPath.SetText(c.JNLPPath)
	u.notes.SetText(c.Notes)
}

func (u *ui) clearForm() {
	u.settingForm = true
	defer func() {
		u.settingForm = false
		u.refreshSelectionState()
	}()
	u.selectedIdx = -1
	u.name.SetText("")
	u.group.SetText("")
	u.target.SetText("")
	u.host.SetText("")
	u.user.SetText("")
	u.password.SetText("")
	u.profile.SetSelected("auto")
	u.certMode.SetSelected("default")
	u.timeMode.SetSelected("off")
	u.jnlpPath.SetText("")
	u.notes.SetText("")
}

func (u *ui) updateJNLPVisibility() {
	if u.jnlpRow == nil {
		return
	}
	if strings.EqualFold(u.profile.Selected, "jnlp") {
		u.jnlpRow.Show()
		return
	}
	u.jnlpRow.Hide()
}

func (u *ui) updateNotesHeight(text string) {
	rows := 1
	if strings.Contains(text, "\n") || len([]rune(text)) > 90 {
		rows = 3
	}
	u.notes.SetMinRowsVisible(rows)
	u.notes.Refresh()
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func scaleOptions() []string {
	return []string{
		"50%", "60%", "70%", "80%", "90%", "100%", "110%", "120%", "130%", "140%",
		"150%", "160%", "170%", "180%", "190%", "200%", "custom...",
	}
}

func stringInSlice(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func redactArgsForLog(args []string) string {
	redacted := append([]string(nil), args...)
	for i := 0; i < len(redacted)-1; i++ {
		key := strings.ToLower(redacted[i])
		if key == "-password" {
			redacted[i+1] = "********"
		}
	}
	return strings.Join(redacted, " ")
}

type scaledTheme struct {
	base       fyne.Theme
	scale      float32
	dirtyFocus bool
}

func (t scaledTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNamePrimary:
		if t.dirtyFocus {
			return color.NRGBA{R: 0xe4, G: 0xb4, B: 0x2e, A: 0xff}
		}
	case theme.ColorNameSuccess:
		return color.NRGBA{R: 0x32, G: 0x8a, B: 0x5a, A: 0xff}
	case theme.ColorNameForegroundOnSuccess:
		return color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	}
	return t.base.Color(name, variant)
}

func (t scaledTheme) Font(style fyne.TextStyle) fyne.Resource {
	return t.base.Font(style)
}

func (t scaledTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return t.base.Icon(name)
}

func (t scaledTheme) Size(name fyne.ThemeSizeName) float32 {
	scale := t.scale
	if scale <= 0 {
		scale = 1
	}
	return t.base.Size(name) * scale
}

type compactFieldLayout struct {
	gap          float32
	labelYOffset float32
}

func (l *compactFieldLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) == 0 {
		return fyne.NewSize(0, 0)
	}
	labelSize := objects[0].MinSize()
	if len(objects) == 1 {
		return labelSize
	}
	fieldSize := objects[1].MinSize()
	fieldY := maxFloat32(0, l.labelYOffset+labelSize.Height+l.gap)
	return fyne.NewSize(maxFloat32(labelSize.Width, fieldSize.Width), fieldY+fieldSize.Height)
}

func (l *compactFieldLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 {
		return
	}
	labelSize := objects[0].MinSize()
	objects[0].Move(fyne.NewPos(0, l.labelYOffset))
	objects[0].Resize(fyne.NewSize(size.Width, labelSize.Height))
	if len(objects) == 1 {
		return
	}
	fieldY := maxFloat32(0, l.labelYOffset+labelSize.Height+l.gap)
	objects[1].Move(fyne.NewPos(0, fieldY))
	objects[1].Resize(fyne.NewSize(size.Width, maxFloat32(0, size.Height-fieldY)))
}

type formLogLayout struct {
	formContent fyne.CanvasObject
	logMin      float32
	gap         float32
}

func (l *formLogLayout) MinSize(_ []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(300, 300)
}

func (l *formLogLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 {
		return
	}
	if len(objects) == 1 {
		objects[0].Move(fyne.NewPos(0, 0))
		objects[0].Resize(size)
		return
	}
	gap := l.gap
	if gap < 0 {
		gap = 0
	}
	logMin := l.logMin
	if logMin <= 0 {
		logMin = 120
	}
	formNeed := objects[0].MinSize().Height
	if l.formContent != nil {
		formNeed = l.formContent.MinSize().Height
	}

	availableForForm := size.Height - logMin - gap
	if availableForForm < 90 {
		availableForForm = maxFloat32(0, size.Height*0.52-gap)
	}
	formH := minFloat32(formNeed, availableForForm)
	if formH < 90 && size.Height > 90+gap {
		formH = minFloat32(90, maxFloat32(0, size.Height-gap))
	}
	logH := size.Height - formH - gap
	if logH < 0 {
		logH = 0
	}

	objects[0].Move(fyne.NewPos(0, 0))
	objects[0].Resize(fyne.NewSize(size.Width, formH))
	objects[1].Move(fyne.NewPos(0, formH+gap))
	objects[1].Resize(fyne.NewSize(size.Width, logH))
}

type fixedMinLayout struct {
	min fyne.Size
}

func (l *fixedMinLayout) MinSize(_ []fyne.CanvasObject) fyne.Size {
	return l.min
}

func (l *fixedMinLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	for _, obj := range objects {
		obj.Move(fyne.NewPos(0, 0))
		obj.Resize(size)
	}
}

func minFloat32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func maxFloat32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}
