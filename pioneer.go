// Package main provides ...
package main

import (
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"time"

	"github.com/distributed/sers"
)

func bind(path string, c Command) {
	bindGenerator(path, func(r *http.Request) Command {
		return c
	})
}

func bindGenerator(path string, generator func(*http.Request) Command) {
	http.DefaultServeMux.Handle(path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		ec := make(chan error)
		cmd := generator(r)
		if cmd == nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		cmdChan <- CmdReq{cmd, ec}
		err := <-ec
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
}

type CmdReq struct {
	Command
	ret chan error
}

var cmdChan chan CmdReq

func main() {
	quit := make(chan struct{})
	sigChan := make(chan os.Signal)
	signal.Notify(sigChan, os.Interrupt, os.Kill)

	cmdChan = make(chan CmdReq)
	serialPort, err := sers.Open("/dev/ttyUSB0")
	if err != nil {
		panic(err)
	}
	err = serialPort.SetMode(9600, 8, sers.N, 1, sers.NO_HANDSHAKE)
	if err != nil {
		panic(err)
	}

	cmdReadWriter := NewCommandReadWriter(serialPort)

	bind("/tv/mute", MuteCommand(true))
	bind("/tv/unmute", MuteCommand(false))
	bindGenerator("/tv/power", func(r *http.Request) Command {
		return PowerCommand(r.FormValue("v") == "1")
	})
	bindGenerator("/tv/volume", func(r *http.Request) Command {
		dir := r.FormValue("d")
		val, e := strconv.Atoi(r.FormValue("v"))
		if e != nil {
			return nil
		}
		if dir == "up" {
			return VolumeUpCommand(val)
		} else if dir == "down" {
			return VolumeDownCommand(val)
		} else {
			return VolumeCommand(val)
		}
	})
	bindGenerator("/tv/input", func(r *http.Request) Command {
		inp, e := strconv.Atoi(r.FormValue("v"))
		if e != nil {
			return nil
		}
		return InputCommand(inp)
	})
	go func() {
		for {
			select {
			case cr := <-cmdChan:
				e := cmdReadWriter.WriteCommand(cr.Command)
				if e != nil {
					cr.ret <- e
				}
				time.Sleep(cr.Command.Delay())
				cr.ret <- cmdReadWriter.ReadResponse()
			case _ = <-sigChan:
				quit <- struct{}{}
				return
			}
		}
	}()
	// T V in hex
	go func() {
		http.ListenAndServe(":5456", nil)
	}()
	<-quit
	serialPort.Close()
}
