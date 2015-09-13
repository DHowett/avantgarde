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
type cmdDigraph struct {
	command1, command2 byte
}

const (
	RKVolumeUp   remoteKey = 2
	RKVolumeDown remoteKey = 3
)

var (
	cmdSetPower            = cmdDigraph{'k', 'a'}
	cmdSetVolume           = cmdDigraph{'k', 'f'}
	cmdRemoteKey           = cmdDigraph{'m', 'c'}
	cmdSetMute             = cmdDigraph{'k', 'e'}
	cmdSetOSD              = cmdDigraph{'k', 'l'}
	cmdSetInput            = cmdDigraph{'x', 'b'}
	cmdSetTuning           = cmdDigraph{'m', 'a'}
	cmdSetScreenMute       = cmdDigraph{'k', 'd'}
	cmdSetContrast         = cmdDigraph{'k', 'g'}
	cmdSetBrightness       = cmdDigraph{'k', 'h'}
	cmdSetColor            = cmdDigraph{'k', 'i'}
	cmdSetTint             = cmdDigraph{'k', 'j'}
	cmdSetSharpness        = cmdDigraph{'k', 'k'}
	cmdSetAudioBalance     = cmdDigraph{'k', 't'}
	cmdSetColorTemperature = cmdDigraph{'k', 'u'}
	cmdSetBacklight        = cmdDigraph{'m', 'g'}
	cmdSetLock             = cmdDigraph{'k', 'm'}
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
	D    cmdDigraph
	data interface{}
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
	b[0] = []byte{c.D.command1, c.D.command2}
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
		return &lgCommand{cmdSetTuning, lgTuningInfo{uint8(cht), 0, 0, 0x01}}
	case tv.DigitalChannel:
		return &lgCommand{cmdSetTuning, lgTuningInfo{0x00, uint16(cht.Ch), uint16(cht.Sub), 0x22}}
	default:
		return nil
	}
}

func clamp(val uint8) uint8 {
	switch {
	case val < 0:
		return 0
	case val > 100:
		return 100
	default:
		return uint8(val)
	}
}

func (lg *lgTV) Do(op *tv.Op) error {
	var cmd *lgCommand
	switch op.Attribute {
	case tv.Power:
		cmd = &lgCommand{cmdSetPower, op.Value}
	case tv.Volume:
		switch op.Operator {
		case tv.Set:
			cmd = &lgCommand{cmdSetVolume, clamp(uint8(op.Value.(int)))}
		case tv.Increment:
			cmd = &lgCommand{cmdRemoteKey, RKVolumeUp}
		case tv.Decrement:
			cmd = &lgCommand{cmdRemoteKey, RKVolumeDown}
		}
	case tv.Mute:
		cmd = &lgCommand{cmdSetMute, op.Value}
	case tv.OSD:
		cmd = &lgCommand{cmdSetOSD, op.Value}
	case tv.Input:
		cmd = &lgCommand{cmdSetInput, uint8(op.Value.(int))}
	case tv.Tuning:
		cmd = lg.channelTuningCommand(op.Value.(tv.Tune))
	case tv.Screen:
		// Screen mute is the opposite of the "screen" avantgarde.tv value
		cmd = &lgCommand{cmdSetScreenMute, !(op.Value.(bool))}
	case tv.Contrast:
		cmd = &lgCommand{cmdSetContrast, clamp(uint8(op.Value.(int)))}
	case tv.Brightness:
		cmd = &lgCommand{cmdSetBrightness, clamp(uint8(op.Value.(int)))}
	case tv.Color:
		cmd = &lgCommand{cmdSetColor, clamp(uint8(op.Value.(int)))}
	case tv.Tint:
		cmd = &lgCommand{cmdSetTint, clamp(uint8(op.Value.(int)))}
	case tv.Sharpness:
		cmd = &lgCommand{cmdSetSharpness, clamp(uint8(op.Value.(int)))}
	case tv.AudioBalance:
		cmd = &lgCommand{cmdSetAudioBalance, clamp(uint8(op.Value.(int)))}
	case tv.ColorTemperature:
		cmd = &lgCommand{cmdSetColorTemperature, clamp(uint8(op.Value.(int)))}
	case tv.Backlight:
		cmd = &lgCommand{cmdSetBacklight, clamp(uint8(op.Value.(int)))}
	case tv.Lock:
		cmd = &lgCommand{cmdSetLock, op.Value}
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
