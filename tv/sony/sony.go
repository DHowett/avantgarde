package sony

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"

	"github.com/DHowett/avantgarde/tv"
)

type remoteKey int

const (
	RKVolumeUp   remoteKey = 30
	RKVolumeDown remoteKey = 31
)

type Config struct {
	Address string
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

	bravia := &braviaTV{
		config: braviac,
		cmds:   make(chan *braviaRespondableWrapper, 1),
		commandResponseQueues: make(map[string]*responseQueue),
	}
	go bravia.run()

	return bravia, nil
}

func (l *braviaModel) NewConfig() tv.Config {
	return &Config{}
}

type braviaTV struct {
	config                *Config
	cmds                  chan *braviaRespondableWrapper
	state                 tv.State
	macAddr               [6]byte
	commandResponseQueues map[string]*responseQueue
}

type braviaEnquiry struct {
	command string
	data    string
}

func (c *braviaEnquiry) ID() string {
	return c.command
}

func (c *braviaEnquiry) Serialize() []byte {
	if c.data == "" {
		return []byte(fmt.Sprintf("*SE%s################\x0a", c.command))
	}
	return []byte(fmt.Sprintf("*SE%s%-16s\x0a", c.command, c.data))
}

type braviaCommand struct {
	command string
	data    interface{}
}

func (c *braviaCommand) ID() string {
	return c.command
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

func (bravia *braviaTV) channelTuningCommand(t tv.Tune) *braviaCommand {
	switch cht := t.C.(type) {
	case tv.AnalogChannel:
		return &braviaCommand{"CHNN", fmt.Sprintf("%08u.0000000", cht)}
	case tv.DigitalChannel:
		return &braviaCommand{"CHNN", fmt.Sprintf("%08u.%07u", cht.Ch, cht.Sub)}
	default:
		return nil
	}
}

var inputTypeMap = map[tv.Connection]int{
	tv.Coaxial:   0,
	tv.Component: 4,
	tv.Composite: 3,
	tv.HDMI:      1,
	tv.SCART:     2,
	tv.PC:        6,
	tv.Special:   5,
}

var reverseInputTypeMap = map[int]tv.Connection{
	0: tv.Coaxial,
	4: tv.Component,
	3: tv.Composite,
	1: tv.HDMI,
	2: tv.SCART,
	6: tv.PC,
	5: tv.Special,
}

func (bravia *braviaTV) inputCommand(i tv.InputNumber) *braviaCommand {
	inputType, ok := inputTypeMap[i.Connection]
	if !ok {
		return nil
	}

	return &braviaCommand{"INPT", fmt.Sprintf("%08u%08u", inputType, i.Number)}
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

type braviaRespondableWrapper struct {
	respondable braviaSendable
	ch          chan error
}

type braviaSendable interface {
	ID() string
	Serialize() []byte
}

func (tv *braviaTV) send(s braviaSendable) chan error {
	respCh := make(chan error)
	tv.cmds <- &braviaRespondableWrapper{s, respCh}
	return respCh
}

func (bravia *braviaTV) Do(op *tv.Op) error {
	var cmd *braviaCommand
	switch op.Attribute {
	case tv.Power:
		cmd = &braviaCommand{"POWR", op.Value}
	case tv.Volume:
		switch op.Operator {
		case tv.Set:
			cmd = &braviaCommand{"VOLU", clamp(uint8(op.Value.(int)))}
		case tv.Increment:
			cmd = &braviaCommand{"IRCC", RKVolumeUp}
		case tv.Decrement:
			cmd = &braviaCommand{"IRCC", RKVolumeDown}
		}
	case tv.Mute:
		cmd = &braviaCommand{"AMUT", op.Value}
	case tv.Screen:
		// Screen mute is the opposite of the "screen" avantgarde.tv value
		switch op.Operator {
		case tv.Set:
			cmd = &braviaCommand{"PMUT", !(op.Value.(bool))}
		case tv.Toggle:
			cmd = &braviaCommand{"TPMU", "################"}
		}
	case tv.Input:
		cmd = bravia.inputCommand(op.Value.(tv.InputNumber))
	case tv.Tuning:
		cmd = bravia.channelTuningCommand(op.Value.(tv.Tune))
	case tv.PIP:
		switch op.Operator {
		case tv.Set:
			cmd = &braviaCommand{"PIPI", op.Value.(bool)}
		case tv.Toggle:
			cmd = &braviaCommand{"TPIP", "################"}
		}
	case tv.Raw:
		buf := op.Value.([]byte)
		if buf[len(buf)-1] != 0x0A {
			buf = append(buf, 0x0A)
		}
		//bravia.w.Write(buf)
		return nil
	}

	if cmd == nil {
		return errors.New("bravia: unsupported")
	} else {
		<-bravia.send(cmd)
		return nil
	}
}

func (tv *braviaTV) State() (*tv.State, error) {
	return nil, errors.New("bravia: unsupported")
}

func (tv *braviaTV) queueFor(cmd string) *responseQueue {
	rq, ok := tv.commandResponseQueues[cmd]
	if !ok {
		rq = &responseQueue{}
		tv.commandResponseQueues[cmd] = rq
	}
	return rq
}

func (tv *braviaTV) parseResponse(resp string) {
	if len(resp) < 24 {
		return
	}

	typ := resp[2]
	cmd := resp[3:7]
	val := resp[7:23]

	var respCh chan error
	if typ == 'A' {
		respCh = tv.queueFor(cmd).Pop()
	}

	if val == "FFFFFFFFFFFFFFFF" {
		//last command was an error
		if respCh != nil {
			respCh <- errors.New("invalid command")
			close(respCh)
		}
		return
	}

	switch cmd {
	case "POWR":
		bval, _ := strconv.ParseInt(val, 10, 0)
		tv.state.Power = bval == int64(1)
	case "VOLU":
		vval, _ := strconv.ParseInt(val, 10, 0)
		tv.state.Volume = int(vval)
	case "AMUT":
		bval, _ := strconv.ParseInt(val, 10, 0)
		tv.state.Mute = bval == int64(1)
	case "PMUT":
		bval, _ := strconv.ParseInt(val, 10, 0)
		tv.state.Screen = bval != int64(1)
	case "INPT":
		ival, _ := strconv.ParseInt(val[0:7], 10, 0)
		nval, _ := strconv.ParseInt(val[8:], 10, 0)
		tv.state.Input.Connection = reverseInputTypeMap[int(ival)]
		tv.state.Input.Number = int(nval)
	case "MADR":
		//val := val[0:11]
	}
	fmt.Printf("%s, %s, %+v\n", cmd, val, tv.state)
	if respCh != nil {
		respCh <- nil
		close(respCh)
	}
}

func (tv *braviaTV) run() {
	for {
		conn, err := net.Dial("tcp", tv.config.Address+":20060")
		if err != nil {
			panic(err)
		}

		respCh := make(chan string, 1)
		errorCh := make(chan error, 1)

		go func() {
			br := bufio.NewReader(conn)
			for {
				resp, err := br.ReadString(0x0A)
				if err != nil {
					errorCh <- err
					return
				}
				respCh <- resp
			}
		}()

		go func() {
			<-tv.send(&braviaEnquiry{command: "MADR", data: "eth0############"})
			<-tv.send(&braviaEnquiry{command: "POWR"})
			<-tv.send(&braviaEnquiry{command: "VOLU"})
			<-tv.send(&braviaEnquiry{command: "AMUT"})
			<-tv.send(&braviaEnquiry{command: "PMUT"})
			<-tv.send(&braviaEnquiry{command: "INPT"})
		}()

		for {
			select {
			case _ = <-errorCh:
				conn.Close()
				close(respCh)
				break
			case resp := <-respCh:
				tv.parseResponse(resp)
			case wrapper := <-tv.cmds:
				id := wrapper.respondable.ID()
				tv.queueFor(id).Push(wrapper.ch)
				_, err := conn.Write(wrapper.respondable.Serialize())
				if err != nil {
					errorCh <- err
				}
			}
		}
	}
}

func init() {
	tv.RegisterModel("bravia", &braviaModel{})
}
