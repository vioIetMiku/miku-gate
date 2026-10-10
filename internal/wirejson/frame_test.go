package wirejson

import (
	"bytes"
	"strings"
	"testing"
)

func TestBoundedUnambiguousFrames(t *testing.T) {
	type message struct {
		Operation string `json:"operation"`
	}
	var b bytes.Buffer
	if Write(&b, message{"inspect"}, 128) != nil {
		t.Fatal("encode")
	}
	var got message
	if Read(&b, &got, 128) != nil || got.Operation != "inspect" {
		t.Fatal("roundtrip")
	}
	for _, raw := range []string{`{"operation":"a","operation":"b"}`, `{"Operation":"x"}`, `{"operation":"x","unknown":true}`, `{"operation":"x"} {}`, `[{}]`, `{"operation":` + strings.Repeat(`[`, 10) + `0` + strings.Repeat(`]`, 10) + `}`} {
		if Decode([]byte(raw), &got, 512) == nil {
			t.Fatal("ambiguous/invalid JSON accepted", raw)
		}
	}
	if Read(bytes.NewReader([]byte{0, 4, 0, 0}), &got, 128) == nil {
		t.Fatal("oversize allocation accepted")
	}
}
func FuzzDecode(f *testing.F) {
	f.Add([]byte(`{"operation":"dial"}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		var message struct {
			Operation string `json:"operation"`
		}
		_ = Decode(raw, &message, 4096)
	})
}
