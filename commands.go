package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

type Command interface {
	Representation() []string
	Delay() time.Duration
}

type StringCommand []string

func (self StringCommand) Representation() []string {
	return []string(self)
}

func (self StringCommand) Delay() time.Duration { return time.Duration(0) }

type DelayedCommand struct {
	Command
	D time.Duration
}

func (self DelayedCommand) Representation() []string {
	return self.Command.Representation()
}

func (self DelayedCommand) Delay() time.Duration {
	return self.D
}

/* Power Commands */
func PowerCommand(on bool) Command {
	if on {
		return DelayedCommand{StringCommand{"PON"}, 2 * time.Second}
	} else {
		return StringCommand{"POF"}
	}
}

/* Input Commands */
func InputCommand(val int) Command {
	if val < 0 || val > 7 {
		return nil
	}
	return DelayedCommand{StringCommand{"INP", fmt.Sprintf("S%2.02d", val)}, 500 * time.Millisecond}
}

/* Volume Commands */
func VolumeCommand(val int) Command {
	if val < 0 || val > 100 {
		return nil
	}
	return StringCommand{"VOL", fmt.Sprintf("%3.03d", val)}
}

func VolumeUpCommand(val int) Command {
	if val < 0 {
		return nil
	}
	if val > 10 {
		val = 0
	}
	return StringCommand{"VOL", fmt.Sprintf("UP%1.01d", val)}
}

func VolumeMaxCommand() Command {
	return StringCommand{"VOL", "UPF"}
}

func VolumeDownCommand(val int) Command {
	if val < 0 {
		return nil
	}
	if val > 10 {
		val = 0
	}
	return StringCommand{"VOL", fmt.Sprintf("DW%1.01d", val)}
}

func VolumeMinCommand() Command {
	return StringCommand{"VOL", "DWF"}
}

func MuteCommand(on bool) Command {
	if on {
		return StringCommand{"AMT", "S01"}
	} else {
		return StringCommand{"AMT", "S00"}
	}
}

type Channel interface {
	Representation() string
}

func ParseChannel(s string) Channel {
	ch, err := strconv.ParseUint(s, 10, 0)
	if err != nil { // This might be a digital channel
		parts := strings.Split(s, ".")
		if len(parts) != 2 {
			return nil
		}
		ch, err = strconv.ParseUint(parts[0], 10, 0)
		if err != nil { // This is not a channel :P
			return nil
		}
		subch, err := strconv.ParseUint(parts[1], 10, 0)
		if err != nil { // This is not a channel :P
			return nil
		}
		return DigitalChannel{uint(ch), uint(subch)}
	}
	return AnalogChannel(uint(ch))
}

type AnalogChannel uint

func (a AnalogChannel) Representation() string {
	return fmt.Sprintf("%3.03d", uint(a))
}

type DigitalChannel struct {
	Ch  uint
	Sub uint
}

func (d DigitalChannel) Representation() string {
	return fmt.Sprintf("%6.06d%3.03d", d.Ch, d.Sub)
}

type Antenna string

const (
	AntennaA Antenna = "A"
	AntennaB         = "B"
)

func TuneChannelCommand(ant Antenna, ch Channel) Command {
	// Antenna B cannot tune digital channels.
	if _, ok := ch.(DigitalChannel); ok && ant == AntennaB {
		return nil
	}
	return StringCommand{"IN" + string(ant), ch.Representation()}
}

/* End of Commands */
type CommandReadWriter struct {
	w io.Writer
	r *bufio.Reader
}

func (self *CommandReadWriter) WriteCommand(c Command) error {
	buf := []byte{0x02, '*', '*'}
	for _, v := range c.Representation() {
		buf = append(buf, []byte(v)...)
	}
	buf = append(buf, 0x03)
	_, err := self.w.Write(buf)
	if err != nil {
		return err
	}
	return nil
}

func (self *CommandReadWriter) ReadResponse() error {
	_, err := self.r.ReadBytes(0x02)
	if err != nil {
		return err
	}
	v, err := self.r.ReadBytes(0x03)
	if err != nil {
		return err
	}
	if bytes.Equal(v, []byte{'E', 'R', 'R', 0x03}) {
		return errors.New("bad command")
	}
	return nil
}

func NewCommandReadWriter(rw io.ReadWriter) *CommandReadWriter {
	return &CommandReadWriter{
		w: rw,
		r: bufio.NewReader(rw),
	}
}
