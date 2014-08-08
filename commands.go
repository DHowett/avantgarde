package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"time"
)

type Command interface {
	Representation() []string
	Delay() time.Duration
}

type BasicCommand string

func (self BasicCommand) Representation() []string {
	return []string{string(self)}
}

func (self BasicCommand) Delay() time.Duration { return time.Duration(0) }

type ArgumentCommand []string

func (self ArgumentCommand) Representation() []string {
	return []string(self)
}

func (self ArgumentCommand) Delay() time.Duration { return time.Duration(0) }

type CommandFunc func() Command

func (self CommandFunc) Representation() []string {
	return self().Representation()
}
func (self CommandFunc) Delay() time.Duration { return self().Delay() }

type DelayCommand struct {
	Command
	D time.Duration
}

func (self DelayCommand) Representation() []string {
	return self.Command.Representation()
}

func (self DelayCommand) Delay() time.Duration {
	return self.D
}

func PowerCommand(on bool) Command {
	if on {
		return DelayCommand{BasicCommand("PON"), 2 * time.Second}
	} else {
		return BasicCommand("POF")
	}
}

func InputCommand(val int) Command {
	if val < 0 || val > 6 {
		return nil
	}
	return DelayCommand{ArgumentCommand{"INP", fmt.Sprintf("S%2.02d", val)}, 500 * time.Millisecond}
}

func VolumeCommand(val int) Command {
	if val < 0 || val > 100 {
		return nil
	}
	return ArgumentCommand{"VOL", fmt.Sprintf("%3.03d", val)}
}

func VolumeUpCommand(val int) Command {
	if val < 0 {
		return nil
	}
	if val > 10 {
		val = 0
	}
	return ArgumentCommand{"VOL", fmt.Sprintf("UP%1.01d", val)}
}

func VolumeDownCommand(val int) Command {
	if val < 0 {
		return nil
	}
	if val > 10 {
		val = 0
	}
	return ArgumentCommand{"VOL", fmt.Sprintf("DW%1.01d", val)}
}

func MuteCommand(on bool) Command {
	if on {
		return ArgumentCommand{"AMT", "S01"}
	} else {
		return ArgumentCommand{"AMT", "S00"}
	}
}

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
