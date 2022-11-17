package module

import (
	"context"
	"fmt"
	"log"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/halliday/go-errors"
)

type Level int

const (
	None Level = 2 << iota
	Info
	Warn
	Error
)

const AllLevels = None | Info | Warn | Error

type Hook func(m *Message) *Message

var GlobalHook Hook

type Message struct {
	Module string `json:"module"`
	Level  Level  `json:"level"`
	*errors.RichError
	Data map[string]interface{} `json:"data"`
}

func New(name string, messages string) (L Logger, E ErrorFactory, m *Module) {
	m = new(Module)
	m.Name = name
	m.messages = messages
	m.Mask = AllLevels
	m.Logger = log.Default()
	return m, m.NewError, m
}

type ErrorFactory func(name string, args ...interface{}) error

type Logger interface {
	Info(name string, args ...interface{})
	Warn(name string, args ...interface{})
	Err(name string, args ...interface{})
	Log(level Level, name string, args ...interface{})

	Printf(desc string, args ...interface{})
	Print(desc string, args ...interface{})

	Report(err error)
}

type Module struct {
	Name     string
	messages string

	Mask   Level
	Hook   Hook
	Logger *log.Logger
	// Stdout io.Writer
	// Stderr io.Writer
}

func (m *Module) NewError(name string, args ...interface{}) error {
	code, desc, link, tail, _, causedBy := m.Lookup(name, args...)
	dataMap := denseArgs(nil, tail)
	var data interface{}
	if len(dataMap) > 0 {
		data = m
	}
	return errors.NewRich(name, code, desc, link, data, causedBy)
}

func (m *Module) Lookup(name string, args ...interface{}) (code int, desc string, link string, tail []interface{}, ctx context.Context, causedBy error) {
	code, pattern, link := m.lookup(name)
	desc, tail, ctx, causedBy = m.format(pattern, args)
	return code, desc, link, tail, ctx, causedBy
}

func (m *Module) format(pattern string, args []interface{}) (desc string, tail []interface{}, ctx context.Context, causedBy error) {
	n := numArgs(pattern)
	if len(args) < n {
		panic(fmt.Sprintf("pattern has %d args for %d placeholders", len(args), n))
	}
	if n != 0 {
		desc = fmt.Sprintf(pattern, args[:n]...)
		args = args[n:]
	} else {
		desc = pattern
	}
	if len(args) > 0 {
		var ok bool
		ctx, ok = args[0].(context.Context)
		if ok {
			args = args[1:]
		}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if len(args) > 0 {
		var ok bool
		causedBy, ok = args[0].(error)
		if ok {
			args = args[1:]
		}
	}
	if len(args) == 0 {
		args = nil
	}
	return desc, args, ctx, causedBy
}

func numArgs(s string) int {
	n := 0
	for {
		i := strings.IndexByte(s, '%')
		if i == -1 {
			return n
		}
		s = s[i+1:]
		if len(s) > 0 && s[0] == '%' {
			s = s[1:]
		} else {
			n++
		}
	}
}

func (m *Module) lookup(name string) (code int, desc string, link string) {
	var line string
	for entries := m.messages; entries != ""; {
		i := strings.IndexByte(entries, '\n')
		if i == -1 {
			line = entries
			entries = ""
		} else {
			line = entries[:i]
			entries = entries[i+1:]
		}
		if len(line) > 0 && line[0] == '#' {
			continue
		}
		i = strings.IndexByte(line, ';')
		if i == -1 {
			continue
		}
		if line[:i] == name {
			line = line[i+1:]
			i = strings.IndexByte(line, ';')
			if i == -1 {
				panic("Module.lookup(\"" + name + "\"): bad line")
			}
			code, err := strconv.Atoi(strings.TrimSpace(line[:i]))
			if err != nil {
				panic("Module.lookup(\"" + name + "\"): bad code")
			}
			line = line[i+1:]
			i = strings.IndexByte(line, ';')
			if i == -1 {
				desc = strings.TrimSpace(line)
			} else {
				desc = strings.TrimSpace(line[:i])
			}
			return code, desc, ""
		}
	}
	panic("Module.lookup(\"" + name + "\"): not found")
}

func (m *Module) Warn(name string, args ...interface{}) {
	m.Log(Warn, name, args...)
}

func (m *Module) Err(name string, args ...interface{}) {
	m.Log(Error, name, args...)
}

func (m *Module) Info(name string, args ...interface{}) {
	m.Log(Info, name, args...)
}

func (m *Module) Printf(pattern string, args ...interface{}) {
	desc, tail, ctx, causedBy := m.format(pattern, args)
	m.log(ctx, None, "", 0, desc, "", tail, causedBy)
}

func (m *Module) Print(msg string, args ...interface{}) {
	var ctx context.Context
	if len(args) > 0 {
		var ok bool
		ctx, ok = args[0].(context.Context)
		if ok {
			args = args[1:]
		}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var causedBy error
	if len(args) > 0 {
		var ok bool
		causedBy, ok = args[0].(error)
		if ok {
			args = args[1:]
		}
	}
	m.log(ctx, None, "", 0, msg, "", args, causedBy)
}

func (m *Module) Report(err error) {
	if err == nil {
		return
	}
	r := errors.Rich(err).(*errors.RichError)
	ctx := context.Background()
	m.log(ctx, Error, r.Name, r.Code, r.Desc, r.Link, []interface{}{r.Data}, r.CausedBy)
}

func (m *Module) Log(level Level, name string, args ...interface{}) {
	code, desc, link, data, ctx, causedBy := m.Lookup(name, args...)
	m.log(ctx, level, name, code, desc, link, data, causedBy)
}

func (m *Module) log(ctx context.Context, level Level, name string, code int, desc string, link string, tail []interface{}, causedBy error) {

	var data map[string]interface{}

	if level&m.Mask != 0 {
		var b strings.Builder
		b.Grow(8 + len(desc))
		switch level {
		case Error:
			b.WriteString("[ERR  ] ")
		case Warn:
			b.WriteString("[WARN ] ")
		case Info:
			b.WriteString("[INFO ] ")
		default:
			b.WriteString("[     ] ")
		}
		b.WriteString(desc)
		data = denseArgs(&b, tail)

		for i := causedBy; i != nil; i = errors.Unwrap(i) {
			b.WriteString(" (caused by ")
			b.WriteString(i.Error())
			b.WriteString(")")
		}

		m.Logger.Println(b.String())
	} else {
		data = denseArgs(nil, tail)
	}

	msg := &Message{
		Module: m.Name,
		Level:  level,
		RichError: &errors.RichError{
			Name:     name,
			Code:     code,
			Desc:     desc,
			Link:     link,
			CausedBy: causedBy,
			Data:     data,
		},
		Data: data,
	}
	if hook := CtxCatch(ctx); hook != nil {
		msg = hook(msg)
	}
	if m.Hook != nil {
		msg = m.Hook(msg)
	}
	if GlobalHook != nil {
		GlobalHook(msg)
	}
}

func denseArg(b *strings.Builder, arg interface{}) (data map[string]interface{}) {
	if tail, ok := arg.([]interface{}); ok {
		return denseArgs(b, tail)
	}
	if data, ok := arg.(map[string]interface{}); ok {
		if b != nil {
			for key, value := range data {
				b.WriteByte(' ')
				b.WriteString(key)
				b.WriteByte('=')
				b.WriteString(EncodeLogValue(fmt.Sprint(value)))
			}
		}
		return data
	}
	return nil
}

func denseArgs(b *strings.Builder, args []interface{}) (data map[string]interface{}) {
	if len(args) == 0 {
		return nil
	}
	if len(args) == 1 {
		return denseArg(b, args[0])
	}
	if len(args)%2 != 0 {
		panic("bad argument count, must be multiple of two")
	}
	data = make(map[string]interface{}, len(args)/2)
	for i := 0; i < len(args); i += 2 {
		key, ok := args[i].(string)
		if !ok {
			panic("bad argument " + strconv.Itoa(i) + ": expected string, found " + reflect.TypeOf(args[i]).Name())
		}
		value := args[i+1]
		data[key] = value
		if b != nil {
			b.WriteByte(' ')
			b.WriteString(key)
			b.WriteByte('=')
			b.WriteString(EncodeLogValue(fmt.Sprint(value)))
		}
	}
	return data
}

var unsafeLogRegexp = regexp.MustCompile(`[\s"]`)

func EncodeLogValue(str string) string {
	if !unsafeLogRegexp.MatchString(str) {
		return str
	}
	var b strings.Builder
	b.Grow(2 + len(str) + strings.Count(str, "\"") + strings.Count(str, "\\"))
	for _, r := range str {
		switch r {
		case '/':
			b.WriteString("\\\\")
		case '"':
			b.WriteString("\\\"")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func Catch(ctx context.Context, hook Hook) context.Context {
	return &catchContext{
		Context: ctx,
		hook:    hook,
	}
}

type catchContextKey struct{}

type catchContext struct {
	context.Context
	hook Hook
}

func (ctx catchContext) Value(key any) any {
	if _, ok := key.(catchContextKey); ok {
		return ctx.hook
	}
	return ctx.Context.Value(key)
}

func CtxCatch(ctx context.Context) (hook Hook) {
	hook, _ = ctx.Value(catchContextKey{}).(Hook)
	return hook
}
