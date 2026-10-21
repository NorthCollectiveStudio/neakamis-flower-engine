package nsh

import (
	"fmt"
	"strings"
)

const (
	CapRead  = 1
	CapWrite = 2
	CapExec  = 4
	CapAdmin = 8
)

type User struct {
	Name    string
	Groups  []string
}

type OptionType int

const (
	OptNone OptionType = iota
	OptBool
	OptInt
	OptFloat
	OptString
)

type Option struct {
	Short    byte
	Long     string
	Type     OptionType
	Required bool
	Help     string
	Caps     uint32
}

type ParsedOption struct {
	Name  string
	IsLong bool
	Value string
}

type CmdFunc func(ctx *Context, args []string, opts map[string]ParsedOption) error

type Command struct {
	Name    string
	Help    string
	Groups  []string
	Options []*Option
	Sub     []*Command
	Run     CmdFunc
	Interact func(ctx *Context, line string) bool
}

type Context struct {
	Shell   *Shell
	User    *User
	Cmd     *Command
	HadHelp bool
}

func (c *Context) OptBool(name string) bool {
	o, ok := c.Shell.parsed[name]
	if !ok {
		return false
	}
	if o.Value == "" {
		return true
	}
	return o.Value == "true" || o.Value == "1" || o.Value == "yes"
}

func (c *Context) OptString(name string) string {
	return c.Shell.parsed[name].Value
}

func (c *Context) OptInt(name string) int {
	v := 0
	fmt.Sscanf(c.Shell.parsed[name].Value, "%d", &v)
	return v
}

func (c *Context) OptFloat(name string) float64 {
	v := 0.0
	fmt.Sscanf(c.Shell.parsed[name].Value, "%g", &v)
	return v
}

func (c *Context) HasOpt(name string) bool {
	_, ok := c.Shell.parsed[name]
	return ok
}

func (o *Option) matchesShort(b byte) bool {
	return o.Short != 0 && o.Short == b
}

func (o *Option) matchesLong(l string) bool {
	return o.Long != "" && o.Long == l
}

func findCommand(cmds []*Command, name string) *Command {
	for _, c := range cmds {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func hasGroup(user *User, groups []string) bool {
	if user == nil {
		return false
	}
	for _, ug := range user.Groups {
		for _, cg := range groups {
			if ug == cg {
				return true
			}
		}
	}
	return false
}

func commandPath(cmds []*Command, target *Command) []string {
	var path []string
	var walk func(list []*Command, acc []string) bool
	walk = func(list []*Command, acc []string) bool {
		for _, c := range list {
			next := append(append([]string{}, acc...), c.Name)
			if c == target {
				path = next
				return true
			}
			if walk(c.Sub, next) {
				return true
			}
		}
		return false
	}
	walk(cmds, nil)
	return path
}

func splitArgs(line string) []string {
	var args []string
	var cur strings.Builder
	inQuote := false
	escaped := false
	for _, r := range line {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case r == '\\':
			escaped = true
		case r == '"':
			inQuote = !inQuote
		case (r == ' ' || r == '\t') && !inQuote:
			if cur.Len() > 0 {
				args = append(args, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		args = append(args, cur.String())
	}
	return args
}
