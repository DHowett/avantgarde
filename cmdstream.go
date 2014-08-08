package main

import "time"

type incomingCommand struct {
	c     Command
	reply chan error
}

type CommandStream struct {
	rw *CommandReadWriter
	c  chan incomingCommand
}

func (self CommandStream) Submit(c Command) chan error {
	reply := make(chan error)
	self.c <- incomingCommand{c, reply}
	return reply
}

func (self CommandStream) Run() {
	go func() {
		for ic := range self.c {
			err := self.rw.WriteCommand(ic.c)
			if err != nil {
				ic.reply <- err
				continue
			}
			time.Sleep(ic.c.Delay())
			ic.reply <- self.rw.ReadResponse()
		}
	}()
}

func NewCommandStream(rw *CommandReadWriter) *CommandStream {
	return &CommandStream{
		rw: rw,
		c:  make(chan incomingCommand),
	}
}
