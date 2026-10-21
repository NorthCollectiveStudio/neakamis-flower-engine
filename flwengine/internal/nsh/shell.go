package nsh

import (
        "fmt"
        "strings"
)

const (
        KeyNone = iota
        KeyUp
        KeyDown
        KeyLeft
        KeyRight
        KeyHome
        KeyEnd
        KeyDelete
        KeyBackspace
        KeyTab
        KeyEnter
        KeyInterrupt
        KeyEof
        KeyBell
)

type History struct {
        entries []string
        max     int
        pos     int
        saved   string
}

func newHistory(max int) *History {
        if max <= 0 {
                max = 64
        }
        return &History{max: max}
}

func (h *History) Add(line string) {
        if line == "" {
                return
        }
        if len(h.entries) > 0 && h.entries[len(h.entries)-1] == line {
                return
        }
        h.entries = append(h.entries, line)
        if len(h.entries) > h.max {
                h.entries = h.entries[len(h.entries)-h.max:]
        }
        h.pos = len(h.entries)
}

func (h *History) Older(current string) (string, bool) {
        if h.pos <= 0 {
                return current, false
        }
        if h.pos == len(h.entries) {
                h.saved = current
        }
        h.pos--
        return h.entries[h.pos], true
}

func (h *History) Newer(current string) (string, bool) {
        if h.pos >= len(h.entries) {
                return current, false
        }
        h.pos++
        if h.pos == len(h.entries) {
                if h.saved != "" {
                        s := h.saved
                        h.saved = ""
                        return s, true
                }
                return current, true
        }
        return h.entries[h.pos], true
}

func (h *History) Reset() {
        h.pos = len(h.entries)
}

func (h *History) Entries() []string {
        return append([]string{}, h.entries...)
}

type LineEditor struct {
        buf     []rune
        cursor  int
        maxLen  int
}

func newLineEditor(maxLen int) *LineEditor {
        if maxLen <= 0 {
                maxLen = 512
        }
        return &LineEditor{maxLen: maxLen}
}

func (le *LineEditor) Set(s string) {
        le.buf = []rune(s)
        if len(le.buf) > le.maxLen {
                le.buf = le.buf[:le.maxLen]
        }
        le.cursor = len(le.buf)
}

func (le *LineEditor) String() string {
        return string(le.buf)
}

func (le *LineEditor) Cursor() int {
        return le.cursor
}

func (le *LineEditor) Insert(r rune) bool {
        if len(le.buf) >= le.maxLen {
                return false
        }
        le.buf = append(le.buf, 0)
        copy(le.buf[le.cursor+1:], le.buf[le.cursor:])
        le.buf[le.cursor] = r
        le.cursor++
        return true
}

func (le *LineEditor) Delete() bool {
        if le.cursor >= len(le.buf) {
                return false
        }
        le.buf = append(le.buf[:le.cursor], le.buf[le.cursor+1:]...)
        return true
}

func (le *LineEditor) Backspace() bool {
        if le.cursor == 0 {
                return false
        }
        le.buf = append(le.buf[:le.cursor-1], le.buf[le.cursor:]...)
        le.cursor--
        return true
}

func (le *LineEditor) Left() bool {
        if le.cursor == 0 {
                return false
        }
        le.cursor--
        return true
}

func (le *LineEditor) Right() bool {
        if le.cursor >= len(le.buf) {
                return false
        }
        le.cursor++
        return true
}

func (le *LineEditor) Home() {
        le.cursor = 0
}

func (le *LineEditor) End() {
        le.cursor = len(le.buf)
}

func (le *LineEditor) KillToEnd() {
        le.buf = le.buf[:le.cursor]
}

func (le *LineEditor) KillToStart() {
        le.buf = le.buf[le.cursor:]
        le.cursor = 0
}

func (le *LineEditor) WordLeft() {
        i := le.cursor
        for i > 0 && le.buf[i-1] == ' ' {
                i--
        }
        for i > 0 && le.buf[i-1] != ' ' {
                i--
        }
        le.cursor = i
}

func (le *LineEditor) WordRight() {
        i := le.cursor
        for i < len(le.buf) && le.buf[i] == ' ' {
                i++
        }
        for i < len(le.buf) && le.buf[i] != ' ' {
                i++
        }
        le.cursor = i
}

func (le *LineEditor) DeleteWordBack() {
        start := le.cursor
        le.WordLeft()
        le.buf = append(le.buf[:le.cursor], le.buf[start:]...)
}

type parsedLine struct {
        tokens []string
        cursorToken int
        beforeCursor string
}

func (le *LineEditor) analyze() parsedLine {
        s := le.String()
        var tokens []string
        var cur strings.Builder
        for _, r := range s {
                if r == ' ' {
                        if cur.Len() > 0 {
                                tokens = append(tokens, cur.String())
                                cur.Reset()
                        }
                } else {
                        cur.WriteRune(r)
                }
        }
        atEnd := le.cursor >= len(le.buf)
        if cur.Len() > 0 {
                tokens = append(tokens, cur.String())
        }
        idx := len(tokens)
        if !atEnd {
                idx = 0
                count := 0
                for i, r := range s {
                        if i >= le.cursor {
                                break
                        }
                        if r == ' ' {
                                idx = count
                        } else if i == 0 || s[i-1] == ' ' {
                                count++
                                idx = count
                        }
                }
                if idx > len(tokens) {
                        idx = len(tokens)
                }
        }
        before := ""
        if le.cursor <= len(s) {
                before = s[:le.cursor]
        }
        return parsedLine{tokens: tokens, cursorToken: idx, beforeCursor: before}
}

type Shell struct {
        Prompt   string
        user     *User
        root     []*Command
        history  *History
        editor   *LineEditor
        parsed   map[string]ParsedOption
        out      OutputSink
        interact *Command
        lastErr  error
        promptWaitMsg  string
        promptWaitRetries int
}

type OutputSink interface {
        Write(s string)
        WriteLine(s string)
}

func New(out OutputSink) *Shell {
        sh := &Shell{
                Prompt:  "\x1b[36mnugget\x1b[0m:\x1b[34m~\x1b[0m$ ",
                history: newHistory(128),
                editor:  newLineEditor(1024),
                parsed:  map[string]ParsedOption{},
        }
        if out == nil {
                out = discardSink{}
        }
        sh.out = out
        sh.user = &User{Name: "dev", Groups: []string{"dev", "admin", "user"}}
        return sh
}

func (s *Shell) Register(cmd *Command) {
        s.root = append(s.root, cmd)
}

func (s *Shell) User() *User {
        return s.user
}

func (s *Shell) Output() OutputSink {
        return s.out
}

func (s *Shell) SetUser(u *User) {
        s.user = u
}

func (s *Shell) FeedRune(r rune) {
        s.editor.Insert(r)
}

func (s *Shell) FeedKey(key int) {
        switch key {
        case KeyLeft:
                s.editor.Left()
        case KeyRight:
                s.editor.Right()
        case KeyHome:
                s.editor.Home()
        case KeyEnd:
                s.editor.End()
        case KeyBackspace:
                s.editor.Backspace()
        case KeyDelete:
                s.editor.Delete()
        case KeyUp:
                if line, ok := s.history.Older(s.editor.String()); ok {
                        s.editor.Set(line)
                }
        case KeyDown:
                if line, ok := s.history.Newer(s.editor.String()); ok {
                        s.editor.Set(line)
                }
        case KeyInterrupt:
                if s.interact != nil {
                        s.interact = nil
                        s.out.WriteLine("^c")
                } else {
                        s.editor.KillToEnd()
                        s.out.WriteLine("")
                }
        case KeyEof:
                s.out.WriteLine("exit")
        }
}

func (s *Shell) FeedControl(seq []byte) {
        key := parseEscape(seq)
        if key != KeyNone {
                s.FeedKey(key)
                return
        }
        if len(seq) == 1 && seq[0] == 0x01 {
                s.editor.Home()
        } else if len(seq) == 1 && seq[0] == 0x05 {
                s.editor.End()
        } else if len(seq) == 1 && seq[0] == 0x0b {
                s.editor.KillToEnd()
        } else if len(seq) == 1 && seq[0] == 0x15 {
                s.editor.KillToStart()
        } else if len(seq) == 1 && seq[0] == 0x17 {
                s.editor.DeleteWordBack()
        } else if len(seq) == 1 && seq[0] == 0x7f {
                s.editor.Backspace()
        } else if len(seq) == 1 && seq[0] == '\t' {
                s.tabComplete()
        }
}

func parseEscape(seq []byte) int {
        if len(seq) < 2 || seq[0] != 0x1b {
                return KeyNone
        }
        if seq[1] == '[' || seq[1] == 'O' {
                if len(seq) < 3 {
                        return KeyNone
                }
                switch seq[2] {
                case 'A':
                        return KeyUp
                case 'B':
                        return KeyDown
                case 'C':
                        return KeyRight
                case 'D':
                        return KeyLeft
                case 'H':
                        return KeyHome
                case 'F':
                        return KeyEnd
                case '3':
                        if len(seq) >= 4 && seq[3] == '~' {
                                return KeyDelete
                        }
                }
        }
        if seq[1] == 0x1b {
                return KeyInterrupt
        }
        return KeyNone
}

func (s *Shell) tabComplete() {
        pl := s.editor.analyze()
        if len(pl.tokens) == 0 || (len(pl.tokens) == 1 && pl.cursorToken == 1) {
                var names []string
                prefix := ""
                if len(pl.tokens) == 1 {
                        prefix = pl.tokens[0]
                }
                for _, c := range s.root {
                        if strings.HasPrefix(c.Name, prefix) {
                                names = append(names, c.Name)
                        }
                }
                if len(names) == 1 {
                        line := names[0] + " "
                        s.editor.Set(line)
                        return
                }
                if len(names) > 1 {
                        s.out.WriteLine(strings.Join(names, "  "))
                }
                return
        }
        var cmds []*Command = s.root
        var target *Command
        for i, t := range pl.tokens {
                if strings.HasPrefix(t, "-") {
                        break
                }
                c := findCommand(cmds, t)
                if c == nil {
                        break
                }
                target = c
                cmds = c.Sub
                if i == len(pl.tokens)-1 && pl.cursorToken == len(pl.tokens) {
                        cmds = c.Sub
                        target = c
                }
        }
        if target == nil {
                return
        }
        last := pl.tokens[len(pl.tokens)-1]
        if strings.HasPrefix(last, "-") && pl.cursorToken == len(pl.tokens) {
                var opts []string
                isLong := strings.HasPrefix(last, "--")
                for _, o := range target.Options {
                        if isLong {
                                if o.Long != "" {
                                        opts = append(opts, "--"+o.Long)
                                }
                        } else if o.Short != 0 {
                                opts = append(opts, "-"+string(rune(o.Short)))
                        }
                }
                var matches []string
                for _, o := range opts {
                        if strings.HasPrefix(o, last) {
                                matches = append(matches, o)
                        }
                }
                if len(matches) == 1 {
                        replace := pl.tokens[:len(pl.tokens)-1]
                        replace = append(replace, matches[0])
                        replace = append(replace, "")
                        s.editor.Set(strings.Join(replace[:len(replace)-1], " ") + " ")
                        return
                }
                if len(matches) > 1 {
                        s.out.WriteLine(strings.Join(matches, "  "))
                }
                return
        }
        if len(cmds) > 0 {
                var subs []string
                for _, c := range cmds {
                        if strings.HasPrefix(c.Name, last) {
                                subs = append(subs, c.Name)
                        }
                }
                if len(subs) == 1 {
                        tokens := append([]string{}, pl.tokens[:len(pl.tokens)-1]...)
                        tokens = append(tokens, subs[0]+" ")
                        s.editor.Set(strings.Join(tokens, " "))
                        return
                }
                if len(subs) > 1 {
                        s.out.WriteLine(strings.Join(subs, "  "))
                }
        }
}

func (s *Shell) Submit() {
        line := strings.TrimSpace(s.editor.String())
        s.editor.Set("")
        s.out.WriteLine("")
        if line == "" {
                return
        }
        s.history.Add(line)
        s.Execute(line)
}

func (s *Shell) CurrentLine() string {
        return s.editor.String()
}

func (s *Shell) Execute(line string) error {
        s.parsed = map[string]ParsedOption{}
        args := splitArgs(line)
        if len(args) == 0 {
                return nil
        }
        if args[0] == "help" || args[0] == "?" {
                s.printHelp(s.root, "commands")
                return nil
        }
        cmd, rest := s.resolve(args)
        if cmd == nil {
                s.out.WriteLine(fmt.Sprintf("unknown command: %s", args[0]))
                return fmt.Errorf("unknown command")
        }
        if !hasGroup(s.user, cmd.Groups) && len(cmd.Groups) > 0 {
                s.out.WriteLine("permission denied")
                return fmt.Errorf("permission denied")
        }
        if err := s.parseOptions(cmd, rest); err != nil {
                s.out.WriteLine(err.Error())
                return err
        }
        if s.parsed["help"].Value == "1" || s.parsed["h"].Value == "1" {
                s.printCmdHelp(cmd)
                return nil
        }
        for _, o := range cmd.Options {
                if o.Required && !s.optPresent(o) {
                        s.out.WriteLine(fmt.Sprintf("missing required option --%s", o.Long))
                        return fmt.Errorf("missing option")
                }
        }
        ctx := &Context{Shell: s, User: s.user, Cmd: cmd}
        if cmd.Run == nil {
                s.printCmdHelp(cmd)
                return nil
        }
        err := cmd.Run(ctx, rest.positional(), s.parsed)
        s.lastErr = err
        return err
}

func (s *Shell) optPresent(o *Option) bool {
        if _, ok := s.parsed[o.Long]; ok && o.Long != "" {
                return true
        }
        if o.Short != 0 {
                key := string(rune(o.Short))
                if _, ok := s.parsed[key]; ok {
                        return true
                }
        }
        return false
}

type restArgs struct {
        raw   []string
}

func (r *restArgs) positional() []string {
        var out []string
        for _, a := range r.raw {
                if !strings.HasPrefix(a, "-") {
                        out = append(out, a)
                }
        }
        return out
}

func (s *Shell) resolve(args []string) (*Command, *restArgs) {
        cmds := s.root
        var found *Command
        i := 0
        for i < len(args) {
                if strings.HasPrefix(args[i], "-") {
                        break
                }
                c := findCommand(cmds, args[i])
                if c == nil {
                        break
                }
                found = c
                cmds = c.Sub
                i++
        }
        if found == nil {
                return nil, &restArgs{raw: args}
        }
        return found, &restArgs{raw: args[i:]}
}

func (s *Shell) parseOptions(cmd *Command, rest *restArgs) error {
        if helpOpt(cmd) == nil {
                cmd.Options = append(cmd.Options, &Option{Short: 'h', Long: "help", Type: OptBool, Help: "show help"})
        }
        for i := 0; i < len(rest.raw); i++ {
                a := rest.raw[i]
                if !strings.HasPrefix(a, "-") {
                        continue
                }
                var opt *Option
                var name string
                var value string
                isLong := strings.HasPrefix(a, "--")
                if isLong {
                        body := strings.TrimPrefix(a, "--")
                        if idx := strings.IndexByte(body, '='); idx >= 0 {
                                name = body[:idx]
                                value = body[idx+1:]
                        } else {
                                name = body
                        }
                        for _, o := range cmd.Options {
                                if o.matchesLong(name) {
                                        opt = o
                                        break
                                }
                        }
                } else {
                        body := strings.TrimPrefix(a, "-")
                        if body == "" {
                                continue
                        }
                        ch := body[0]
                        value = body[1:]
                        for _, o := range cmd.Options {
                                if o.matchesShort(ch) {
                                        opt = o
                                        break
                                }
                        }
                }
                if opt == nil {
                        s.out.WriteLine(fmt.Sprintf("unknown option: %s", a))
                        return fmt.Errorf("unknown option")
                }
                if value == "" && opt.Type != OptBool {
                        if i+1 < len(rest.raw) && !strings.HasPrefix(rest.raw[i+1], "-") {
                                value = rest.raw[i+1]
                                i++
                        } else {
                                s.out.WriteLine(fmt.Sprintf("option %s requires a value", a))
                                return fmt.Errorf("option value")
                        }
                }
                if opt.Type == OptBool {
                        if value == "" {
                                value = "true"
                        }
                }
                key := opt.Long
                if key == "" {
                        key = string(rune(opt.Short))
                }
                s.parsed[key] = ParsedOption{Name: key, Value: value, IsLong: isLong}
                if opt.Short != 0 {
                        s.parsed[string(rune(opt.Short))] = ParsedOption{Name: key, Value: value}
                }
        }
        return nil
}

func helpOpt(cmd *Command) *Option {
        for _, o := range cmd.Options {
                if o.Long == "help" || o.Short == 'h' {
                        return o
                }
        }
        return nil
}

func (s *Shell) printHelp(cmds []*Command, title string) {
        s.out.WriteLine(title + ":")
        for _, c := range cmds {
                s.out.WriteLine(fmt.Sprintf("  %-16s %s", c.Name, c.Help))
        }
}

func (s *Shell) printCmdHelp(cmd *Command) {
        s.out.WriteLine(cmd.Name + " - " + cmd.Help)
        if len(cmd.Sub) > 0 {
                s.out.WriteLine("subcommands:")
                for _, c := range cmd.Sub {
                        s.out.WriteLine(fmt.Sprintf("  %-16s %s", c.Name, c.Help))
                }
        }
        if len(cmd.Options) > 0 {
                s.out.WriteLine("options:")
                for _, o := range cmd.Options {
                        short := "  "
                        if o.Short != 0 {
                                short = "-" + string(rune(o.Short)) + ","
                        }
                        long := ""
                        if o.Long != "" {
                                long = "--" + o.Long
                        }
                        req := ""
                        if o.Required {
                                req = " (required)"
                        }
                        s.out.WriteLine(fmt.Sprintf("  %3s %-14s %-10s %s%s", short, long, typeName(o.Type), o.Help, req))
                }
        }
}

func typeName(t OptionType) string {
        switch t {
        case OptBool:
                return "bool"
        case OptInt:
                return "int"
        case OptFloat:
                return "float"
        case OptString:
                return "string"
        }
        return ""
}

func (s *Shell) History() *History {
        return s.history
}

type discardSink struct{}

func (d discardSink) Write(string)     {}
func (d discardSink) WriteLine(string) {}
