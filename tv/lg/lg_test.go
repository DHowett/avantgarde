package lg

import "bytes"
import "testing"

func TestSerialization(t *testing.T) {
	lgc := &lgCommand{
		'k',
		'a',
		true,
	}

	expect := []byte("ka 01 01\x0D")
	if !bytes.Equal(lgc.Serialize(1), expect) {
		t.Errorf("Got %x instead of %x for serializing %v!", lgc.Serialize(1), expect, lgc)
	}

	lgc = &lgCommand{
		'm',
		'a',
		struct {
			A       uint8
			Ch, Sub uint16
			B       uint8
		}{0, 2, 1, 0x22},
	}

	expect = []byte("ma 01 00 00 02 00 01 22\x0D")
	if !bytes.Equal(lgc.Serialize(1), expect) {
		t.Errorf("Got %x instead of %x for serializing %v!", lgc.Serialize(1), expect, lgc)
	}
}
