package lg

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/DHowett/avantgarde/tv"
)

type remoteKey uint8

const (
	RKVolumeUp   remoteKey = 2
	RKVolumeDown remoteKey = 3
)

type Config struct {
	SetID uint8
}

func (c Config) ModelSpecificRepresentation() interface{} {
	return c
}

type lgModel struct {
}

func (l *lgModel) Initialize(rwc io.ReadWriteCloser, c tv.Config) (tv.TV, error) {
	lgc, ok := c.(*Config)
	if !ok {
		return nil, fmt.Errorf("lg: invalid config type %T", c)
	}

	lg := &lgTV{
		config: lgc,
		r:      bufio.NewReader(rwc),
		w:      rwc,
		c:      rwc,
	}
	lg.run()
	return lg, nil
}

func (l *lgModel) NewConfig() tv.Config {
	return &Config{}
}

type lgCommand struct {
	class, command byte
	data           interface{}
}

func (c *lgCommand) Serialize(SetID uint8) []byte {
	d := c.data
	if v, ok := d.(bool); ok {
		if v {
			d = uint8(1)
		} else {
			d = uint8(0)
		}
	}

	buf := &bytes.Buffer{}
	binary.Write(buf, binary.BigEndian, d)

	b := make([][]byte, 2, buf.Len()+2)
	b[0] = []byte{c.class, c.command}
	b[1] = []byte(fmt.Sprintf("%2.02x", SetID))
	for _, v := range buf.Bytes() {
		b = append(b, []byte(fmt.Sprintf("%2.02x", v)))
	}
	raw := bytes.Join(b, []byte{0x20})
	return append(raw, 0x0D)
}

type lgTV struct {
	config *Config
	r      *bufio.Reader
	w      io.Writer
	c      io.Closer
}

func (lg *lgTV) channelTuningCommand(t tv.Tune) *lgCommand {
	type lgTuningInfo struct {
		Phys                uint8
		Channel, Subchannel uint16
		Antenna             uint8
	}

	switch cht := t.C.(type) {
	case tv.AnalogChannel:
		return &lgCommand{'m', 'a', lgTuningInfo{uint8(cht), 0, 0, 0x01}}
	case tv.DigitalChannel:
		return &lgCommand{'m', 'a', lgTuningInfo{0x00, uint16(cht.Ch), uint16(cht.Sub), 0x22}}
	default:
		return nil
	}
}

func (lg *lgTV) Do(op *tv.Op) error {
	var cmd *lgCommand
	switch op.Attribute {
	case tv.Power:
		cmd = &lgCommand{'k', 'a', op.Value}
	case tv.Mute:
		cmd = &lgCommand{'k', 'e', op.Value}
	case tv.OSD:
		cmd = &lgCommand{'k', 'l', op.Value}
	case tv.Volume:
		switch op.Operator {
		case tv.Set:
			cmd = &lgCommand{'k', 'f', uint8(op.Value.(int))}
		case tv.Increment:
			cmd = &lgCommand{'m', 'c', RKVolumeUp}
		case tv.Decrement:
			cmd = &lgCommand{'m', 'c', RKVolumeDown}
		}
	case tv.Input:
		cmd = &lgCommand{'x', 'b', uint8(op.Value.(int))}
	case tv.Tuning:
		cmd = lg.channelTuningCommand(op.Value.(tv.Tune))
	case tv.Raw:
		buf := op.Value.([]byte)
		if buf[len(buf)-1] != 0x0D {
			buf = append(buf, 0x0D)
		}
		lg.w.Write(buf)
		return nil
	}

	if cmd == nil {
		return errors.New("lg: unsupported")
	} else {
		serialized := cmd.Serialize(lg.config.SetID)
		lg.w.Write(serialized)
		return nil
	}
}

func (tv *lgTV) State() (*tv.State, error) {
	return nil, errors.New("lg: unsupported")
}

func (lg *lgTV) run() {
	go func() {
		for {
			resp, err := lg.r.ReadString('x')
			if err != nil {
				panic(err)
			}
			delim := strings.LastIndex(resp, "\r\n")
			if delim > -1 {
				resp = resp[delim+2:]
			}

			if resp[1] != ' ' {
				continue
			}

			var subCommand byte
			var setId uint8
			var status string
			var data []byte
			n, err := fmt.Sscanf(resp, "%c %2x %2s%xx", &subCommand, &setId, &status, &data)
			if n < 4 || err != nil {
				continue
			}

		}
	}()
}

func init() {
	tv.RegisterModel("lg", &lgModel{})
}
