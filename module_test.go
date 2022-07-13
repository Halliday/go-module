package module

import (
	"bytes"
	_ "embed"
	"log"
	"testing"
)

//go:embed test_messages.csv
var messages string

func TestNew(t *testing.T) {

	var lastMessage *Message
	hook := func(m *Message) *Message {
		lastMessage = m
		return m
	}

	var b bytes.Buffer

	var l, e, m = New("module", messages)
	m.Logger = log.New(&b, "", 0)
	m.Hook = hook

	test2 := e("test2")

	l.Warn("test", test2, "A", 1, "B", "foo")

	if lastMessage == nil {
		t.Fatal("no message was send")
	}
	if lastMessage.Level != Warn || lastMessage.Name != "test" || lastMessage.Code != 123 || lastMessage.Desc != "This is a test message" || lastMessage.CausedBy.Error() != test2.Error() {
		t.Fatal("the message was unexpected")
	}

	l.Err("test3")

	log.Print(b.String())

	if b.String() != "[WARN ] This is a test message A=1 B=foo\n[ERR  ] Some more tests over here.\n" {
		t.Fatal("unexpected log output")
	}
}
