package sony

import (
	"bufio"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"

	"github.com/DHowett/avantgarde/tv"
)

type Config struct {
	Address string
}

func (c Config) ModelSpecificRepresentation() interface{} {
	return c
}

type braviaModel struct{}

func (l *braviaModel) Initialize(rwc io.ReadWriteCloser, c tv.Config) (tv.TV, error) {
	braviac, ok := c.(*Config)
	if !ok {
		return nil, fmt.Errorf("bravia: invalid config type %T", c)
	}

	bravia := newBraviaTV(braviac)
	return bravia, nil
}

func (l *braviaModel) NewConfig() tv.Config {
	return &Config{}
}

type requestWithResponse struct {
	request
	ch chan error
}

type request interface {
	ID() string
	Serialize() []byte
}

type braviaEnquiry struct {
	command string
	data    string
}

func (c *braviaEnquiry) ID() string {
	return c.command
}

func (c *braviaEnquiry) Serialize() []byte {
	value := padRequestRight(c.data, '#')
	return []byte(fmt.Sprintf("*S%c%s%s\x0a", typeEnquiry, c.command, value))
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

	var value string
	if dstr, ok := d.(string); ok && len(dstr) == 16 {
		value = dstr
	} else {
		value = padRequestLeft(fmt.Sprintf("%v", d), '0')
	}
	return []byte(fmt.Sprintf("*S%c%s%s\x0a", typeCommand, c.command, value))
}

type braviaRawCommand []byte

func (c braviaRawCommand) ID() string {
	return string(c[3:7])
}

func (c braviaRawCommand) Serialize() []byte {
	return []byte(c)
}

var tvInputToBravia = map[tv.Connection]uint{
	tv.Coaxial:   0,
	tv.Component: 4,
	tv.Composite: 3,
	tv.HDMI:      1,
	tv.SCART:     2,
	tv.PC:        6,
	tv.Special:   5,
}

var braviaInputToTV = map[int]tv.Connection{
	0: tv.Coaxial,
	1: tv.HDMI,
	2: tv.SCART,
	3: tv.Composite,
	4: tv.Component,
	5: tv.Special,
	6: tv.PC,
}

func channelTuningCommand(t tv.Tune) *braviaCommand {
	switch cht := t.C.(type) {
	case tv.AnalogChannel:
		return &braviaCommand{cmdChannel, fmt.Sprintf("%08d.0000000", cht)}
	case tv.DigitalChannel:
		return &braviaCommand{cmdChannel, fmt.Sprintf("%08d.%07d", cht.Ch, cht.Sub)}
	default:
		return nil
	}
}

func inputCommand(i tv.InputNumber) *braviaCommand {
	inputType, ok := tvInputToBravia[i.Connection]
	if !ok {
		return nil
	}

	return &braviaCommand{cmdInput, fmt.Sprintf("%08d%08d", inputType, i.Number)}
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

type braviaTV struct {
	config  *Config
	reqCh   chan *requestWithResponse
	eventCh chan *tv.Op
	state   tv.State
	macAddr []byte

	commandResponseQueues map[string]*responseQueue
	eventHandlers         map[tv.Attribute][]func(*tv.Op)
}

func newBraviaTV(config *Config) *braviaTV {
	bravia := &braviaTV{
		config:                config,
		reqCh:                 make(chan *requestWithResponse, 1000),
		eventCh:               make(chan *tv.Op, 1000),
		commandResponseQueues: make(map[string]*responseQueue),
		eventHandlers:         make(map[tv.Attribute][]func(*tv.Op)),
	}
	bravia.init()
	go bravia.run()
	return bravia
}

func (tv *braviaTV) send(s request) chan error {
	respCh := make(chan error)
	tv.reqCh <- &requestWithResponse{s, respCh}
	return respCh
}

func (brv *braviaTV) when(ev tv.Attribute, handler func(*tv.Op)) {
	brv.eventHandlers[ev] = append(brv.eventHandlers[ev], handler)
}

func (bravia *braviaTV) Do(op *tv.Op) error {
	var cmd request
	switch op.Attribute {
	case tv.Power:
		cmd = &braviaCommand{cmdPower, op.Value}
	case tv.Volume:
		switch op.Operator {
		case tv.Set:
			cmd = &braviaCommand{cmdVolume, clamp(uint8(op.Value.(int)))}
		case tv.Increment:
			cmd = &braviaCommand{cmdRemoteKey, RKVolumeUp}
		case tv.Decrement:
			cmd = &braviaCommand{cmdRemoteKey, RKVolumeDown}
		}
	case tv.Mute:
		cmd = &braviaCommand{cmdMute, op.Value}
	case tv.Screen:
		// Screen mute is the opposite of the "screen" avantgarde.tv value
		switch op.Operator {
		case tv.Set:
			cmd = &braviaCommand{cmdScreenMute, !(op.Value.(bool))}
		case tv.Toggle:
			cmd = &braviaCommand{cmdToggleScreenMute, requestValueNoData}
		}
	case tv.Input:
		cmd = inputCommand(op.Value.(tv.InputNumber))
	case tv.Tuning:
		cmd = channelTuningCommand(op.Value.(tv.Tune))
	case tv.PIP:
		switch op.Operator {
		case tv.Set:
			cmd = &braviaCommand{cmdPIP, op.Value.(bool)}
		case tv.Toggle:
			cmd = &braviaCommand{cmdTogglePIP, requestValueNoData}
		}
	case tv.Raw:
		buf := op.Value.([]byte)
		if buf[len(buf)-1] != 0x0A {
			buf = append(buf, 0x0A)
		}
		cmd = braviaRawCommand(buf)
	}

	if cmd == nil {
		return errors.New("bravia: unsupported")
	} else {
		err := <-bravia.send(cmd)
		return err
	}
}

func (tv *braviaTV) State() (*tv.State, error) {
	return &tv.state, nil
}

func (tv *braviaTV) responseQueueForCommand(cmd string) *responseQueue {
	rq, ok := tv.commandResponseQueues[cmd]
	if !ok {
		rq = &responseQueue{}
		tv.commandResponseQueues[cmd] = rq
	}
	return rq
}

func (brv *braviaTV) parseResponse(resp string) *tv.Op {
	if len(resp) < 24 {
		return nil
	}

	typ := resp[2]
	cmd := resp[3:7]
	val := resp[7:23]

	var respCh chan error
	if typ == typeAnswer {
		respCh = brv.responseQueueForCommand(cmd).Pop()
	}

	if val == responseValueError {
		//last command was an error
		if respCh != nil {
			respCh <- errors.New("invalid command")
			close(respCh)
		}
		return nil
	}

	op := &tv.Op{Operator: tv.Set}

	switch cmd {
	case cmdPower:
		bval, _ := strconv.ParseInt(val, 10, 0)
		boolval := bval == int64(1)

		op.Attribute = tv.Power
		op.Value = boolval
		brv.state.Power = boolval
	case cmdVolume:
		vval, _ := strconv.ParseInt(val, 10, 0)

		op.Attribute = tv.Volume
		op.Value = int(vval)
		brv.state.Volume = int(vval)
	case cmdMute:
		bval, _ := strconv.ParseInt(val, 10, 0)
		boolval := bval == int64(1)

		op.Attribute = tv.Mute
		op.Value = boolval
		brv.state.Mute = boolval
	case cmdScreenMute:
		bval, _ := strconv.ParseInt(val, 10, 0)
		boolval := bval == int64(1)

		op.Attribute = tv.Screen
		op.Value = !boolval
		brv.state.Screen = !boolval
	case cmdInput:
		ival, _ := strconv.ParseInt(val[0:8], 10, 0)
		nval, _ := strconv.ParseInt(val[8:], 10, 0)

		brv.state.Input.Connection = braviaInputToTV[int(ival)]
		brv.state.Input.Number = int(nval)

		op.Attribute = tv.Input
		op.Value = brv.state.Input
	case cmdMACAddress:
		brv.macAddr, _ = hex.DecodeString(val[0:12])
	}

	if respCh != nil {
		respCh <- nil
		close(respCh)
	}

	return op
}

func (brv *braviaTV) init() {
	brv.when(tv.Power, func(op *tv.Op) {
		if pval, ok := op.Value.(bool); ok && pval {
			go func() {
				brv.send(&braviaEnquiry{command: cmdMute})
				brv.send(&braviaEnquiry{command: cmdScreenMute})
				brv.send(&braviaEnquiry{command: cmdInput})
			}()
		}
	})
	brv.when(tv.Mute, func(op *tv.Op) {
		if pval, ok := op.Value.(bool); ok && !pval {
			go func() {
				brv.send(&braviaEnquiry{command: cmdVolume})
			}()
		}
	})
}

func (brv *braviaTV) run() {
	for {
		conn, err := net.Dial("tcp", brv.config.Address+":20060")
		if err != nil {
			panic(err)
		}

		respCh := make(chan string, 1)
		errorCh := make(chan error, 1)

		go func() {
			// response reader / event generator
			br := bufio.NewReader(conn)
			for {
				resp, err := br.ReadString(0x0A)
				if err != nil {
					errorCh <- err
					return
				}
				event := brv.parseResponse(resp)
				if event != nil {
					brv.eventCh <- event
				}
			}
		}()

		go func() {
			// command dispatcher
			for {
				wrappedRequest, ok := <-brv.reqCh
				if !ok {
					return
				}

				id := wrappedRequest.ID()
				brv.responseQueueForCommand(id).Push(wrappedRequest.ch)
				_, err := conn.Write(wrappedRequest.Serialize())
				if err != nil {
					errorCh <- err
					return
				}
				_, _ = <-wrappedRequest.ch // wait for the command to receive any response
			}
		}()

		// request MAC address and power state
		brv.send(&braviaEnquiry{command: cmdMACAddress, data: "eth0"})
		brv.send(&braviaEnquiry{command: cmdPower})

		for {
			select {
			case _ = <-errorCh:
				conn.Close()
				close(respCh)
				break
			case event := <-brv.eventCh:
				handlers, ok := brv.eventHandlers[event.Attribute]
				if ok {
					for _, handler := range handlers {
						handler(event)
					}
				}
			}
		}
	}
}

func init() {
	tv.RegisterModel("bravia", &braviaModel{})
}
