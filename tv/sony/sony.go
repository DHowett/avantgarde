package sony

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"

	"github.com/DHowett/avantgarde/tv"
)

type remoteKey int

const (
	RKVolumeUp   remoteKey = 30
	RKVolumeDown remoteKey = 31
)

type Config struct {
	Address string
	Port    uint16
}

func (c Config) ModelSpecificRepresentation() interface{} {
	return c
}

type braviaModel struct {
}

func (l *braviaModel) Initialize(rwc io.ReadWriteCloser, c tv.Config) (tv.TV, error) {
	braviac, ok := c.(*Config)
	if !ok {
		return nil, fmt.Errorf("bravia: invalid config type %T", c)
	}

	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%v", braviac.Address, braviac.Port))
	if err != nil {
		return nil, err
	}

	rwc = conn

	bravia := &braviaTV{
		config: braviac,
		r:      bufio.NewReader(rwc),
		w:      rwc,
		c:      rwc,
	}
	return bravia, nil
}

func (l *braviaModel) NewConfig() tv.Config {
	return &Config{}
}

type braviaCommand struct {
	command string
	data    interface{}
}

func (c *braviaCommand) Serialize() []byte {
	d := c.data
	if v, ok := d.(bool); ok {
		if v {
			d = uint8(1)
		} else {
			d = uint8(0)
		}
	}

	return []byte(fmt.Sprintf("*SC%s%016v\x0a", c.command, d))
}

type braviaTV struct {
	config *Config
	r      *bufio.Reader
	w      io.Writer
	c      io.Closer
}

func (bravia *braviaTV) channelTuningCommand(t tv.Tune) *braviaCommand {
	type braviaTuningInfo struct {
		Phys                uint8
		Channel, Subchannel uint16
		Antenna             uint8
	}

	//switch cht := t.C.(type) {
	//case tv.AnalogChannel:
	//return &braviaCommand{'m', 'a', braviaTuningInfo{uint8(cht), 0, 0, 0x01}}
	//case tv.DigitalChannel:
	//return &braviaCommand{'m', 'a', braviaTuningInfo{0x00, uint16(cht.Ch), uint16(cht.Sub), 0x22}}
	//default:
	return nil
	//}
}

func (bravia *braviaTV) Do(op *tv.Op) error {
	var cmd *braviaCommand
	switch op.Attribute {
	case tv.Power:
		cmd = &braviaCommand{"POWR", op.Value}
	case tv.Mute:
		cmd = &braviaCommand{"AMUT", op.Value}
	//case tv.OSD:
	case tv.Volume:
		switch op.Operator {
		case tv.Set:
			cmd = &braviaCommand{"VOLU", uint8(op.Value.(int))}
		case tv.Increment:
			cmd = &braviaCommand{"IRCC", RKVolumeUp}
		case tv.Decrement:
			cmd = &braviaCommand{"IRCC", RKVolumeDown}
		}
	//case tv.Input:
	//cmd = &braviaCommand{"INPT", uint8(op.Value.(int))}
	//case tv.Tuning:
	//cmd = bravia.channelTuningCommand(op.Value.(tv.Tune))
	case tv.Raw:
		buf := op.Value.([]byte)
		if buf[len(buf)-1] != 0x0A {
			buf = append(buf, 0x0A)
		}
		bravia.w.Write(buf)
		return nil
	}

	if cmd == nil {
		return errors.New("bravia: unsupported")
	} else {
		serialized := cmd.Serialize()
		bravia.w.Write(serialized)
		return nil
	}
}

func (tv *braviaTV) State() (*tv.State, error) {
	return nil, errors.New("bravia: unsupported")
}

func init() {
	tv.RegisterModel("bravia", &braviaModel{})
}
